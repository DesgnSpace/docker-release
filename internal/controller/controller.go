package controller

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/malico/docker-release/internal/config"
	"github.com/malico/docker-release/internal/docker"
	"github.com/malico/docker-release/internal/monitor"
	"github.com/malico/docker-release/internal/provider"
	"github.com/malico/docker-release/internal/rollback"
	"github.com/malico/docker-release/internal/state"
	"github.com/malico/docker-release/internal/strategy"

	"github.com/docker/docker/api/types"
)

type activeDeployment struct {
	id     string
	cancel context.CancelFunc
}

type Controller struct {
	docker       *docker.Client
	stateManager *state.Manager
	project      string

	mu             sync.Mutex
	deployments    map[string]activeDeployment
	nginxProxyProv *provider.NginxProxyProvider
	wg             sync.WaitGroup
}

func New(dockerClient *docker.Client, stateManager *state.Manager, project string) *Controller {
	return &Controller{
		docker:       dockerClient,
		stateManager: stateManager,
		deployments:  make(map[string]activeDeployment),
		project:      project,
	}
}

func (c *Controller) Watch(ctx context.Context) error {
	if err := c.docker.Ping(ctx); err != nil {
		return fmt.Errorf("docker not reachable: %w", err)
	}

	log.Println("connected to docker")

	services, err := c.discoverServices(ctx)
	if err != nil {
		return fmt.Errorf("discovering services: %w", err)
	}

	log.Printf("found %d managed service(s)", len(services))
	for name, containers := range services {
		log.Printf("  %s: %d container(s)", name, len(containers))
	}

	msgCh, errCh := c.docker.Events(ctx, c.project)
	commandTicker := time.NewTicker(time.Second)
	defer commandTicker.Stop()

	c.generateInitialConfigs(ctx, services)
	c.processReleaseCommands(ctx)

	log.Println("watching for events... (ctrl+c to stop)")
	for {
		select {
		case <-commandTicker.C:
			c.processReleaseCommands(ctx)
		case msg := <-msgCh:
			switch msg.Action {
			case "create", "start":
				c.handleStart(ctx, msg.Actor.ID, msg.Actor.Attributes)
			case "die", "stop", "destroy":
				c.handleDie(ctx, msg.Actor.ID, msg.Actor.Attributes)
			case "health_status: healthy", "health_status: unhealthy":
				c.handleHealthStatus(ctx, msg.Actor.ID, msg.Actor.Attributes)
			}
		case err := <-errCh:
			if ctx.Err() != nil {
				log.Println("shutting down")
				return nil
			}
			return fmt.Errorf("event stream: %w", err)
		case <-ctx.Done():
			log.Println("shutting down")
			return nil
		}
	}
}

func (c *Controller) EnqueueRelease(service string, force bool) error {
	cmd, err := c.stateManager.EnqueueReleaseCommand(service, force)
	if err != nil {
		return err
	}

	log.Printf("queued detached release for %s (%s)", service, cmd.ID)
	return nil
}

func (c *Controller) processReleaseCommands(ctx context.Context) {
	commands, err := c.stateManager.PendingReleaseCommands()
	if err != nil {
		log.Printf("error reading release commands: %v", err)
		return
	}

	for _, cmd := range commands {
		claimed, ok, err := c.stateManager.ClaimReleaseCommand(cmd)
		if err != nil {
			log.Printf("error claiming release command %s: %v", cmd.ID, err)
			continue
		}
		if !ok {
			continue
		}

		log.Printf("processing detached release for %s (%s)", claimed.Service, claimed.ID)
		if err := c.Release(ctx, claimed.Service, claimed.Force); err != nil {
			log.Printf("detached release failed for %s (%s): %v", claimed.Service, claimed.ID, err)
		}

		if err := c.stateManager.CompleteReleaseCommand(claimed); err != nil {
			log.Printf("error completing release command %s: %v", claimed.ID, err)
		}
	}
}

func (c *Controller) handleDie(ctx context.Context, containerID string, attrs map[string]string) {
	serviceName := c.serviceFromEvent(ctx, containerID, attrs)
	if serviceName == "" {
		c.refreshAllConfigs(ctx)
		return
	}

	exitCode := attrs["exitCode"]

	c.mu.Lock()
	_, deploying := c.deployments[serviceName]
	c.mu.Unlock()

	if deploying {
		log.Printf("container %s died during deployment (service=%s, exit=%s)", containerID[:12], serviceName, exitCode)
		return
	}

	log.Printf("container %s died (service=%s, exit=%s)", containerID[:12], serviceName, exitCode)

	c.refreshServiceConfig(ctx, serviceName)
	c.refreshServiceConfigAfter(ctx, serviceName, 2*time.Second)
}

func (c *Controller) handleStart(ctx context.Context, containerID string, attrs map[string]string) {
	serviceName := c.serviceFromEvent(ctx, containerID, attrs)
	if serviceName == "" {
		return
	}

	log.Printf("container started: %s (service=%s)", containerID[:12], serviceName)

	containers, err := c.docker.ListManagedContainers(ctx, c.project)
	if err != nil {
		log.Printf("error listing containers: %v", err)
		return
	}

	serviceContainers := filterServiceContainers(containers, serviceName)

	if len(serviceContainers) < 2 {
		c.refreshServiceConfig(ctx, serviceName)
		c.refreshServiceConfigAfter(ctx, serviceName, 2*time.Second)
		return
	}

	images := groupByImageID(serviceContainers)

	if len(images) < 2 {
		c.refreshServiceConfig(ctx, serviceName)
		c.refreshServiceConfigAfter(ctx, serviceName, 2*time.Second)
		return
	}

	old, new := separateOldAndNew(images, containerID)

	if len(old) == 0 || len(new) == 0 {
		c.refreshServiceConfig(ctx, serviceName)
		c.refreshServiceConfigAfter(ctx, serviceName, 2*time.Second)
		return
	}

	ds, err := c.stateManager.Load(serviceName)
	if err != nil {
		log.Printf("error loading state for %s: %v", serviceName, err)
		return
	}

	if ds.Status == state.StatusInProgress {
		if !ds.IsStale(state.DefaultStaleThreshold) {
			log.Printf("deployment already in progress for %s, skipping", serviceName)
			return
		}

		log.Printf("clearing stale deployment state for %s (last updated: %s)", serviceName, formatTimestamp(ds.UpdatedAt))
	}

	cfg, err := config.ParseLabels(new[0].Labels)
	if err != nil {
		log.Printf("error parsing labels for %s: %v", serviceName, err)
		return
	}

	c.resolveNginxProxyUpstream(ctx, cfg, new)

	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		c.deploy(ctx, serviceName, cfg, old, new)
	}()
}

func (c *Controller) deploy(parentCtx context.Context, serviceName string, cfg *config.ServiceConfig, oldContainers, newContainers []types.Container) {
	c.mu.Lock()
	if d, ok := c.deployments[serviceName]; ok {
		d.cancel()
	}

	ctx, cancel := context.WithCancel(parentCtx)
	deployID := state.GenerateDeploymentID()
	c.deployments[serviceName] = activeDeployment{id: deployID, cancel: cancel}
	c.mu.Unlock()

	ds := &state.DeploymentState{
		Service:  serviceName,
		Status:   state.StatusInProgress,
		Strategy: string(cfg.Strategy),
	}
	if err := c.stateManager.Save(ds); err != nil {
		log.Printf("error saving early state for %s: %v", serviceName, err)
	}

	defer func() {
		c.mu.Lock()
		if d, ok := c.deployments[serviceName]; ok && d.id == deployID {
			delete(c.deployments, serviceName)
		}
		c.mu.Unlock()
		cancel()
	}()

	log.Printf("starting %s deployment for %s", cfg.Strategy, serviceName)

	expected := len(oldContainers)
	if len(newContainers) < expected {
		newContainers = c.waitForContainers(ctx, serviceName, newContainers[0].ImageID, expected)
	}

	prov := c.createProvider(cfg, serviceName)

	resolveAddr := cfg.Provider != config.ProviderNone

	oldInfos, err := c.resolveContainers(ctx, oldContainers, resolveAddr)
	if err != nil {
		log.Printf("error resolving old containers for %s: %v", serviceName, err)
		return
	}

	newInfos, err := c.resolveContainers(ctx, newContainers, resolveAddr)
	if err != nil {
		log.Printf("error resolving new containers for %s: %v", serviceName, err)
		return
	}

	d := &strategy.Deployment{
		Service: serviceName,
		Config:  cfg,
		Old:     oldInfos,
		New:     newInfos,
	}

	deployCtx, deployCancel := context.WithCancel(ctx)
	defer deployCancel()

	newIDs := make([]string, len(newInfos))
	for i, info := range newInfos {
		newIDs[i] = info.ID
	}

	strat := c.createStrategy(cfg, prov, c.docker)

	mon := monitor.NewHealthMonitor(c.docker, newIDs, func(containerID, reason string) {
		log.Printf("auto-rollback triggered for %s: %s", serviceName, reason)
		deployCancel()
	})
	mon.SetGracePeriod(cfg.HealthCheckTimeout)

	go mon.Run(deployCtx)

	if err := strat.Execute(deployCtx, d); err != nil {
		log.Printf("deployment failed for %s: %v", serviceName, err)

		rollbackCtx, rollbackCancel := context.WithTimeout(context.Background(), cfg.HealthCheckTimeout+cfg.DrainTimeout+30*time.Second)
		defer rollbackCancel()

		log.Printf("initiating abort rollback for %s", serviceName)
		if rbErr := c.abortDeployment(rollbackCtx, serviceName, cfg, prov, d); rbErr != nil {
			log.Printf("rollback failed for %s: %v", serviceName, rbErr)
		}
		return
	}

	log.Printf("deployment complete for %s", serviceName)
}

func (c *Controller) abortDeployment(ctx context.Context, serviceName string, cfg *config.ServiceConfig, prov provider.Provider, d *strategy.Deployment) error {
	targets, err := c.abortTargets(ctx, serviceName, d)
	if err != nil {
		return err
	}
	if len(targets) == 0 {
		return fmt.Errorf("no live rollback targets for %s", serviceName)
	}

	upstream := &provider.UpstreamState{
		Service:      serviceName,
		UpstreamName: d.UpstreamName(),
		Affinity:     cfg.Affinity,
	}

	for _, target := range targets {
		upstream.Servers = append(upstream.Servers, provider.Server{Addr: target.Addr})
	}
	applyProviderKeepalive(cfg, upstream)

	if err := prov.GenerateConfig(upstream); err != nil {
		return fmt.Errorf("generating abort rollback config: %w", err)
	}

	if err := prov.Reload(); err != nil {
		return fmt.Errorf("reloading abort rollback config: %w", err)
	}

	select {
	case <-time.After(cfg.DrainTimeout):
	case <-ctx.Done():
		return ctx.Err()
	}

	targetIDs := make(map[string]bool, len(targets))
	for _, target := range targets {
		targetIDs[target.ID] = true
	}

	for _, newContainer := range d.New {
		if targetIDs[newContainer.ID] {
			continue
		}

		if err := c.docker.Stop(ctx, newContainer.ID, 10); err != nil {
			log.Printf("abort rollback warning: stop %s: %v", newContainer.ID[:12], err)
		}

		if err := c.docker.Remove(ctx, newContainer.ID); err != nil {
			log.Printf("abort rollback warning: remove %s: %v", newContainer.ID[:12], err)
		}
	}

	return c.stateManager.Save(&state.DeploymentState{
		Service:    serviceName,
		Status:     state.StatusIdle,
		Strategy:   string(cfg.Strategy),
		Containers: state.Containers{Stable: containerInfoIDs(targets)},
	})
}

func (c *Controller) abortTargets(ctx context.Context, serviceName string, d *strategy.Deployment) ([]strategy.ContainerInfo, error) {
	containersByID := make(map[string]strategy.ContainerInfo, len(d.Old)+len(d.New))
	for _, info := range d.Old {
		containersByID[info.ID] = info
	}
	for _, info := range d.New {
		containersByID[info.ID] = info
	}

	ds, err := c.stateManager.Load(serviceName)
	if err != nil {
		return nil, fmt.Errorf("loading abort state: %w", err)
	}

	ids := ds.Containers.Stable
	if len(ids) == 0 {
		ids = containerInfoIDs(d.Old)
	}

	targets := make([]strategy.ContainerInfo, 0, len(ids))
	for _, id := range ids {
		info, ok := containersByID[id]
		if !ok {
			continue
		}
		if _, err := c.docker.Inspect(ctx, info.ID); err != nil {
			continue
		}
		targets = append(targets, info)
	}

	return targets, nil
}

func containerInfoIDs(containers []strategy.ContainerInfo) []string {
	ids := make([]string, len(containers))
	for i, container := range containers {
		ids[i] = container.ID
	}
	return ids
}

func (c *Controller) resolveNginxProxyUpstream(ctx context.Context, cfg *config.ServiceConfig, containers []types.Container) {
	if cfg.Provider != config.ProviderNginxProxy || cfg.UpstreamName != "" || len(containers) == 0 {
		return
	}
	env, err := c.docker.ContainerEnv(ctx, containers[0].ID)
	if err != nil {
		log.Printf("warning: could not read container env for nginx-proxy upstream: %v", err)
		return
	}
	name, err := provider.NginxProxyUpstreamName(env)
	if err != nil {
		log.Printf("warning: could not resolve nginx-proxy upstream name: %v", err)
		return
	}
	cfg.UpstreamName = name
}

func (c *Controller) createProvider(cfg *config.ServiceConfig, serviceName string) provider.Provider {
	switch cfg.Provider {
	case config.ProviderNginx:
		return provider.NewNginx(cfg.NginxConfigDir, c.docker, cfg.NginxService, c.project)
	case config.ProviderAngie:
		return provider.NewAngie(cfg.AngieConfigDir, c.docker, cfg.AngieService, c.project)
	case config.ProviderTraefik:
		return provider.NewTraefik(cfg.TraefikConfigDir)
	case config.ProviderNginxProxy:
		return c.getNginxProxyProvider(cfg)
	case config.ProviderCaddy:
		return provider.NewCaddy(cfg.CaddyConfigDir, c.docker, cfg.CaddyService, c.project)
	case config.ProviderHAProxy:
		return provider.NewHAProxy(cfg.HAProxyConfigDir, c.docker, cfg.HAProxyService, c.project)
	case config.ProviderNone:
		return provider.NewNoop()
	default:
		return provider.NewNginx(cfg.NginxConfigDir, c.docker, cfg.NginxService, c.project)
	}
}

func (c *Controller) getNginxProxyProvider(cfg *config.ServiceConfig) provider.Provider {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.nginxProxyProv != nil {
		return c.nginxProxyProv
	}

	tmplPath := cfg.NginxConfigDir + "/nginx.tmpl"
	prov, err := provider.NewNginxProxy(tmplPath)
	if err != nil {
		log.Printf("warning: could not load nginx-proxy template at %s: %v", tmplPath, err)
		return provider.NewNoop()
	}

	c.nginxProxyProv = prov
	return prov
}

func (c *Controller) createStrategy(cfg *config.ServiceConfig, prov provider.Provider, dockerOps strategy.DockerOps) strategy.Strategy {
	switch cfg.Strategy {
	case config.StrategyBlueGreen:
		return strategy.NewBlueGreen(dockerOps, prov, c.stateManager)
	case config.StrategyCanary:
		return strategy.NewCanary(dockerOps, prov, c.stateManager)
	default:
		return strategy.NewLinear(dockerOps, prov, c.stateManager)
	}
}

func (c *Controller) waitForContainers(ctx context.Context, serviceName, imageID string, expected int) []types.Container {
	timeout := 30 * time.Second
	deadline := time.After(timeout)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	log.Printf("waiting for %d new container(s) for %s (have fewer)", expected, serviceName)

	for {
		select {
		case <-deadline:
			log.Printf("timed out waiting for %d new containers for %s, proceeding with what's available", expected, serviceName)
			return c.listContainersByImage(ctx, serviceName, imageID)
		case <-ctx.Done():
			return c.listContainersByImage(ctx, serviceName, imageID)
		case <-ticker.C:
			found := c.listContainersByImage(ctx, serviceName, imageID)
			if len(found) >= expected {
				log.Printf("found %d/%d new container(s) for %s", len(found), expected, serviceName)
				return found
			}
		}
	}
}

func (c *Controller) listContainersByImage(ctx context.Context, serviceName, imageID string) []types.Container {
	containers, err := c.docker.ListManagedContainers(ctx, c.project)
	if err != nil {
		return nil
	}

	serviceContainers := filterServiceContainers(containers, serviceName)
	matched := filterByImageID(serviceContainers, imageID)

	return matched
}

func (c *Controller) resolveContainers(ctx context.Context, containers []types.Container, resolveAddr bool) ([]strategy.ContainerInfo, error) {
	var infos []strategy.ContainerInfo

	for _, ctr := range containers {
		info := strategy.ContainerInfo{ID: ctr.ID}

		if resolveAddr {
			addr, err := c.docker.ContainerAddr(ctx, ctr.ID)
			if err != nil {
				log.Printf("warning: resolving %s: %v", ctr.ID[:12], err)
				continue
			}
			info.Addr = addr
		}

		infos = append(infos, info)
	}

	return infos, nil
}

func (c *Controller) Release(ctx context.Context, service string, force bool) error {
	ds, err := c.stateManager.Load(service)
	if err != nil {
		return fmt.Errorf("loading state: %w", err)
	}

	if ds.Status == state.StatusInProgress && !ds.IsStale(state.DefaultStaleThreshold) && !force {
		return fmt.Errorf("deployment already in progress for %q (started %s) — use --force to override", service, formatTimestamp(ds.UpdatedAt))
	}

	containers, err := c.docker.ListManagedContainers(ctx, c.project)
	if err != nil {
		return fmt.Errorf("listing containers: %w", err)
	}

	serviceContainers := filterServiceContainers(containers, service)

	if len(serviceContainers) == 0 {
		return fmt.Errorf("no managed containers found for service %q", service)
	}

	revisions := groupByRevision(serviceContainers)

	if len(revisions) >= 2 {
		oldContainers, newContainers := splitByRevision(serviceContainers, revisions)
		cfg, err := config.ParseLabels(newContainers[0].Labels)
		if err != nil {
			return fmt.Errorf("parsing labels: %w", err)
		}

		c.resolveNginxProxyUpstream(ctx, cfg, newContainers)

		log.Printf("releasing %s: %d old → %d new", service, len(oldContainers), len(newContainers))
		c.wg.Add(1)
		go func() {
			defer c.wg.Done()
			c.deploy(ctx, service, cfg, oldContainers, newContainers)
		}()
		return nil
	}

	cfg, err := config.ParseLabels(serviceContainers[0].Labels)
	if err != nil {
		return fmt.Errorf("parsing labels: %w", err)
	}

	c.resolveNginxProxyUpstream(ctx, cfg, serviceContainers)

	newContainers, err := c.scaleUp(ctx, serviceContainers)
	if err != nil {
		return fmt.Errorf("scaling up: %w", err)
	}

	log.Printf("releasing %s: %d old → %d new", service, len(serviceContainers), len(newContainers))
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		c.deploy(ctx, service, cfg, serviceContainers, newContainers)
	}()

	return nil
}

func (c *Controller) WaitDeployments() {
	c.wg.Wait()
}

func (c *Controller) scaleUp(ctx context.Context, existing []types.Container) ([]types.Container, error) {
	log.Printf("scaling up: creating %d container(s) from image", len(existing))

	var project, service string
	if len(existing) > 0 {
		project = existing[0].Labels["com.docker.compose.project"]
		service = existing[0].Labels["com.docker.compose.service"]
	}
	maxNum := c.docker.MaxServiceContainerNumber(ctx, project, service)

	var newIDs []string
	for i, ctr := range existing {
		newID, err := c.docker.CreateContainerFromImage(ctx, ctr, maxNum+1+i)
		if err != nil {
			for _, id := range newIDs {
				_ = c.docker.Remove(context.Background(), id)
			}
			return nil, err
		}
		newIDs = append(newIDs, newID)
	}

	newIDSet := make(map[string]bool, len(newIDs))
	for _, id := range newIDs {
		newIDSet[id] = true
	}

	allContainers, err := c.docker.ListManagedContainers(ctx, c.project)
	if err != nil {
		return nil, fmt.Errorf("listing containers: %w", err)
	}

	var newContainers []types.Container
	for _, ctr := range allContainers {
		if newIDSet[ctr.ID] {
			newContainers = append(newContainers, ctr)
		}
	}

	return newContainers, nil
}

func splitByRevision(containers []types.Container, revisions map[string][]types.Container) (old, new []types.Container) {
	var newestTime int64
	var newestRevision string
	for _, ctr := range containers {
		if ctr.Created > newestTime {
			newestTime = ctr.Created
			newestRevision = containerRevision(ctr)
		}
	}

	for revision, ctrs := range revisions {
		if revision == newestRevision {
			new = ctrs
		} else {
			old = append(old, ctrs...)
		}
	}

	return old, new
}

func filterServiceContainers(containers []types.Container, serviceName string) []types.Container {
	var matched []types.Container
	for _, container := range containers {
		if container.Labels["com.docker.compose.service"] == serviceName {
			matched = append(matched, container)
		}
	}

	return matched
}

func (c *Controller) serviceFromEvent(ctx context.Context, containerID string, attrs map[string]string) string {
	if c.project != "" {
		eventProject := attrs["com.docker.compose.project"]
		if eventProject == "" {
			info, err := c.docker.Inspect(ctx, containerID)
			if err != nil {
				return ""
			}
			if info.Config != nil && info.Config.Labels != nil {
				eventProject = info.Config.Labels["com.docker.compose.project"]
			}
		}
		if eventProject != c.project {
			return ""
		}
	}

	serviceName := attrs["com.docker.compose.service"]
	if serviceName != "" {
		return serviceName
	}

	info, err := c.docker.Inspect(ctx, containerID)
	if err != nil {
		return ""
	}

	if info.Config == nil {
		return ""
	}

	if info.Config.Labels == nil {
		return ""
	}

	return info.Config.Labels["com.docker.compose.service"]
}

func filterByImageID(containers []types.Container, imageID string) []types.Container {
	var matched []types.Container
	for _, container := range containers {
		if container.ImageID == imageID {
			matched = append(matched, container)
		}
	}

	return matched
}

func groupByImageID(containers []types.Container) map[string][]types.Container {
	grouped := make(map[string][]types.Container)
	for _, container := range containers {
		grouped[container.ImageID] = append(grouped[container.ImageID], container)
	}

	return grouped
}

func groupByRevision(containers []types.Container) map[string][]types.Container {
	grouped := make(map[string][]types.Container)
	for _, container := range containers {
		revision := containerRevision(container)
		grouped[revision] = append(grouped[revision], container)
	}

	return grouped
}

func containerRevision(container types.Container) string {
	if hash := container.Labels["com.docker.compose.config-hash"]; hash != "" {
		return "config:" + hash
	}

	return "image:" + container.ImageID
}

func separateOldAndNew(images map[string][]types.Container, newContainerID string) (oldContainers []types.Container, newContainers []types.Container) {
	for _, containers := range images {
		if containsContainer(containers, newContainerID) {
			newContainers = containers
			continue
		}

		oldContainers = append(oldContainers, containers...)
	}

	return oldContainers, newContainers
}

func containsContainer(containers []types.Container, targetID string) bool {
	for _, container := range containers {
		if container.ID == targetID {
			return true
		}
	}

	return false
}

func (c *Controller) Rollback(ctx context.Context, service string) error {
	coord := rollback.NewCoordinator(c.stateManager, c.docker)

	cfg := c.resolveServiceConfig(ctx, service)
	prov := c.createProvider(cfg, service)
	coord.RegisterStrategy("linear", strategy.NewLinear(c.docker, prov, c.stateManager))
	coord.RegisterStrategy("blue-green", strategy.NewBlueGreen(c.docker, prov, c.stateManager))
	coord.RegisterStrategy("canary", strategy.NewCanary(c.docker, prov, c.stateManager))

	return coord.Execute(ctx, service)
}

func (c *Controller) resolveServiceConfig(ctx context.Context, service string) *config.ServiceConfig {
	containers, err := c.docker.ListManagedContainers(ctx, c.project)
	if err != nil {
		return &config.ServiceConfig{Provider: config.ProviderNone}
	}

	for _, ctr := range containers {
		if ctr.Labels["com.docker.compose.service"] == service {
			cfg, err := config.ParseLabels(ctr.Labels)
			if err != nil {
				continue
			}
			return cfg
		}
	}

	return &config.ServiceConfig{Provider: config.ProviderNone}
}

func (c *Controller) Status(ctx context.Context, service string) error {
	if service == "" {
		return c.statusAll(ctx)
	}

	s, err := c.stateManager.Load(service)
	if err != nil {
		return fmt.Errorf("loading state: %w", err)
	}

	statusStr := string(s.Status)
	if s.IsStale(state.DefaultStaleThreshold) {
		statusStr += " (stale)"
	}

	fmt.Printf("Service:    %s\n", s.Service)
	fmt.Printf("Status:     %s\n", statusStr)
	fmt.Printf("Strategy:   %s\n", s.Strategy)
	fmt.Printf("Updated:    %s\n", formatTimestamp(s.UpdatedAt))
	fmt.Printf("Weight:     %d%%\n", s.CurrentWeight)
	fmt.Printf("Stable:     %v\n", s.Containers.Stable)
	fmt.Printf("Canary:     %v\n", s.Containers.Canary)

	return nil
}

func (c *Controller) statusAll(ctx context.Context) error {
	containers, err := c.docker.ListManagedContainers(ctx, c.project)
	if err != nil {
		return fmt.Errorf("listing containers: %w", err)
	}

	services := make(map[string]bool)
	for _, ctr := range containers {
		name := ctr.Labels["com.docker.compose.service"]
		if name != "" {
			services[name] = true
		}
	}

	if len(services) == 0 {
		fmt.Println("no managed services found")
		return nil
	}

	for name := range services {
		s, err := c.stateManager.Load(name)
		if err != nil {
			log.Printf("error loading state for %s: %v", name, err)
			continue
		}

		status := string(s.Status)
		if status == "" {
			status = "idle"
		}

		if s.IsStale(state.DefaultStaleThreshold) {
			status += " (stale)"
		}

		fmt.Printf("%-20s %s\n", name, status)
	}

	return nil
}

func (c *Controller) discoverServices(ctx context.Context) (map[string][]types.Container, error) {
	containers, err := c.docker.ListManagedContainers(ctx, c.project)
	if err != nil {
		return nil, err
	}

	services := make(map[string][]types.Container)
	for _, ctr := range containers {
		name := ctr.Labels["com.docker.compose.service"]
		if name == "" {
			continue
		}
		services[name] = append(services[name], ctr)
	}

	return services, nil
}

func (c *Controller) generateInitialConfigs(ctx context.Context, services map[string][]types.Container) {
	activeConfigs := make(map[string]*config.ServiceConfig)

	for name, containers := range services {
		if len(containers) == 0 {
			continue
		}

		cfg, err := config.ParseLabels(containers[0].Labels)
		if err != nil {
			log.Printf("skipping initial config for %s: %v", name, err)
			continue
		}

		c.resolveNginxProxyUpstream(ctx, cfg, containers)
		activeConfigs[name] = cfg
	}

	c.cleanStaleConfigs(activeConfigs)

	for name, cfg := range activeConfigs {
		c.generateServiceConfig(ctx, name, cfg, services[name], false)
	}
}

func (c *Controller) cleanStaleConfigs(activeConfigs map[string]*config.ServiceConfig) {
	type configDir struct {
		dir string
		ext string
	}

	active := make(map[configDir]map[string]bool)

	for name, cfg := range activeConfigs {
		var cd configDir

		switch cfg.Provider {
		case config.ProviderNginx:
			cd = configDir{dir: cfg.NginxConfigDir, ext: ".conf"}
		case config.ProviderAngie:
			cd = configDir{dir: cfg.AngieConfigDir, ext: ".conf"}
		case config.ProviderTraefik:
			cd = configDir{dir: cfg.TraefikConfigDir, ext: ".yml"}
		case config.ProviderCaddy:
			cd = configDir{dir: cfg.CaddyConfigDir, ext: ".caddy"}
		case config.ProviderHAProxy:
			cd = configDir{dir: cfg.HAProxyConfigDir, ext: ".cfg"}
		default:
			continue
		}

		if cd.dir == "" {
			continue
		}

		if active[cd] == nil {
			active[cd] = make(map[string]bool)
		}
		active[cd][name] = true
	}

	for cd, services := range active {
		entries, err := os.ReadDir(cd.dir)
		if err != nil {
			continue
		}

		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), cd.ext) {
				continue
			}

			service := strings.TrimSuffix(entry.Name(), cd.ext)
			if services[service] {
				continue
			}

			path := filepath.Join(cd.dir, entry.Name())

			content, err := os.ReadFile(path)
			if err != nil || !strings.HasPrefix(string(content), "# Generated by docker-release") {
				continue
			}

			if err := os.Remove(path); err != nil {
				log.Printf("error removing stale config %s: %v", path, err)
				continue
			}

			log.Printf("removed stale config: %s", path)
		}
	}
}

func (c *Controller) generateServiceConfig(ctx context.Context, name string, cfg *config.ServiceConfig, containers []types.Container, checkHealth bool) {
	prov := c.createProvider(cfg, name)

	upstream, ok := c.deploymentStateUpstream(ctx, name, cfg, containers)
	if !ok {
		upstream = &provider.UpstreamState{
			Service:      name,
			UpstreamName: cfg.UpstreamName,
		}
	}

	if !ok {
		for _, ctr := range containers {
			addr, err := c.docker.ContainerAddr(ctx, ctr.ID)
			if err != nil {
				log.Printf("warning: resolving %s: %v", ctr.ID[:12], err)
				continue
			}

			down := false
			if checkHealth {
				healthy, err := c.docker.IsHealthy(ctx, ctr.ID)
				if err != nil {
					log.Printf("warning: checking health of %s: %v", ctr.ID[:12], err)
					continue
				}
				down = !healthy
			}

			upstream.Servers = append(upstream.Servers, provider.Server{
				Addr: addr,
				Down: down,
			})
		}
	}

	if len(upstream.Servers) == 0 {
		return
	}

	if checkHealth {
		hasActive := false
		for _, s := range upstream.Servers {
			if !s.Down {
				hasActive = true
				break
			}
		}
		if !hasActive {
			return
		}
	}

	applyProviderKeepalive(cfg, upstream)

	if err := prov.GenerateConfig(upstream); err != nil {
		log.Printf("error generating config for %s: %v", name, err)
		return
	}

	if err := prov.Reload(); err != nil {
		log.Printf("error reloading provider for %s: %v", name, err)
		c.refreshServiceConfigAfter(ctx, name, 5*time.Second)
		return
	}

	activeCount := 0
	for _, s := range upstream.Servers {
		if !s.Down {
			activeCount++
		}
	}

	log.Printf("generated config for %s (%d server(s), %d active)", name, len(upstream.Servers), activeCount)
}

func (c *Controller) deploymentStateUpstream(ctx context.Context, name string, cfg *config.ServiceConfig, containers []types.Container) (*provider.UpstreamState, bool) {
	ds, err := c.stateManager.Load(name)
	if err != nil || ds.Status != state.StatusInProgress || len(ds.Containers.Stable) == 0 || len(ds.Containers.Canary) == 0 {
		return nil, false
	}

	weight := ds.CurrentWeight
	if weight <= 0 {
		switch ds.Strategy {
		case "canary":
			weight = cfg.Canary.StartPercentage
		case "blue-green":
			weight = cfg.BlueGreen.GreenWeight
		default:
			return nil, false
		}
	}

	if weight <= 0 || weight > 100 {
		return nil, false
	}

	containersByID := make(map[string]types.Container, len(containers))
	for _, ctr := range containers {
		containersByID[ctr.ID] = ctr
	}

	upstream := &provider.UpstreamState{
		Service:      name,
		UpstreamName: cfg.UpstreamName,
		Affinity:     cfg.Affinity,
	}

	stableWeight := 100 - weight
	c.addWeightedServers(ctx, upstream, containersByID, ds.Containers.Stable, stableWeight, "stable")
	c.addWeightedServers(ctx, upstream, containersByID, ds.Containers.Canary, weight, "canary")

	if len(upstream.Servers) == 0 {
		return nil, false
	}

	applyProviderKeepalive(cfg, upstream)
	return upstream, true
}

func (c *Controller) addWeightedServers(ctx context.Context, upstream *provider.UpstreamState, containersByID map[string]types.Container, ids []string, weight int, group string) {
	for _, id := range ids {
		ctr, ok := containersByID[id]
		if !ok {
			continue
		}

		addr, err := c.docker.ContainerAddr(ctx, ctr.ID)
		if err != nil {
			log.Printf("warning: resolving %s: %v", ctr.ID[:12], err)
			continue
		}

		upstream.Servers = append(upstream.Servers, provider.Server{
			Addr:   addr,
			Weight: weight,
			Group:  group,
		})
	}
}

func applyProviderKeepalive(cfg *config.ServiceConfig, upstream *provider.UpstreamState) {
	if cfg.Provider == config.ProviderNginx || cfg.Provider == config.ProviderNginxProxy {
		upstream.Keepalive = cfg.ResolveNginxKeepalive(len(upstream.Servers))
	}

	if cfg.Provider == config.ProviderAngie {
		upstream.Keepalive = cfg.ResolveAngieKeepalive(len(upstream.Servers))
	}

	if cfg.Provider == config.ProviderCaddy {
		upstream.Keepalive = cfg.ResolveCaddyKeepalive(len(upstream.Servers))
	}
}

func (c *Controller) refreshServiceConfig(ctx context.Context, serviceName string) {
	containers, err := c.docker.ListManagedContainers(ctx, c.project)
	if err != nil {
		log.Printf("error listing containers: %v", err)
		return
	}

	serviceContainers := filterServiceContainers(containers, serviceName)

	if len(serviceContainers) == 0 {
		c.refreshAllConfigs(ctx)
		return
	}

	if c.shouldSkipRefresh(serviceName) {
		return
	}

	cfg, err := config.ParseLabels(serviceContainers[0].Labels)
	if err != nil {
		log.Printf("error parsing labels for %s: %v", serviceName, err)
		return
	}

	c.resolveNginxProxyUpstream(ctx, cfg, serviceContainers)
	c.generateServiceConfig(ctx, serviceName, cfg, serviceContainers, true)
}

func (c *Controller) refreshServiceConfigAfter(ctx context.Context, serviceName string, delay time.Duration) {
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return
		}

		c.refreshServiceConfig(ctx, serviceName)
	}()
}

func (c *Controller) refreshAllConfigs(ctx context.Context) {
	containers, err := c.docker.ListManagedContainers(ctx, c.project)
	if err != nil {
		log.Printf("error listing containers: %v", err)
		return
	}

	services := make(map[string][]types.Container)
	for _, ctr := range containers {
		name := ctr.Labels["com.docker.compose.service"]
		if name == "" {
			continue
		}
		services[name] = append(services[name], ctr)
	}

	activeConfigs := make(map[string]*config.ServiceConfig)
	for name, serviceContainers := range services {
		if len(serviceContainers) == 0 {
			continue
		}

		cfg, err := config.ParseLabels(serviceContainers[0].Labels)
		if err != nil {
			log.Printf("error parsing labels for %s: %v", name, err)
			continue
		}

		activeConfigs[name] = cfg
	}

	c.cleanStaleConfigs(activeConfigs)

	for name, cfg := range activeConfigs {
		if c.shouldSkipRefresh(name) {
			continue
		}
		c.resolveNginxProxyUpstream(ctx, cfg, services[name])
		c.generateServiceConfig(ctx, name, cfg, services[name], true)
	}
}

func (c *Controller) shouldSkipRefresh(serviceName string) bool {
	c.mu.Lock()
	_, deploying := c.deployments[serviceName]
	c.mu.Unlock()

	if deploying {
		return true
	}

	ds, err := c.stateManager.Load(serviceName)
	if err == nil && ds.Status == state.StatusInProgress && !ds.IsStale(state.DefaultStaleThreshold) {
		log.Printf("deployment in progress for %s (from another process), skipping config refresh", serviceName)
		return true
	}

	return false
}

func (c *Controller) handleHealthStatus(ctx context.Context, containerID string, attrs map[string]string) {
	serviceName := c.serviceFromEvent(ctx, containerID, attrs)
	if serviceName == "" {
		return
	}

	log.Printf("health status changed: %s (service=%s)", containerID[:12], serviceName)

	c.refreshServiceConfig(ctx, serviceName)
}

func formatTimestamp(t time.Time) string {
	if t.IsZero() {
		return "unknown"
	}

	return t.Format("2006-01-02 15:04:05")
}
