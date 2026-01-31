package controller

import (
	"context"
	"fmt"
	"log"
	"sync"

	"github.com/malico/docker-release/internal/config"
	"github.com/malico/docker-release/internal/docker"
	"github.com/malico/docker-release/internal/monitor"
	"github.com/malico/docker-release/internal/provider"
	"github.com/malico/docker-release/internal/rollback"
	"github.com/malico/docker-release/internal/state"
	"github.com/malico/docker-release/internal/strategy"

	"github.com/docker/docker/api/types"
)

type Controller struct {
	docker       *docker.Client
	stateManager *state.Manager

	mu             sync.Mutex
	deployments    map[string]context.CancelFunc
	nginxProxyProv *provider.NginxProxyProvider
}

func New(dockerClient *docker.Client, stateManager *state.Manager) *Controller {
	return &Controller{
		docker:       dockerClient,
		stateManager: stateManager,
		deployments:  make(map[string]context.CancelFunc),
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

	c.generateInitialConfigs(ctx, services)

	log.Println("watching for events... (ctrl+c to stop)")

	msgCh, errCh := c.docker.Events(ctx)
	for {
		select {
		case msg := <-msgCh:
			switch msg.Action {
			case "start":
				c.handleStart(ctx, msg.Actor.ID, msg.Actor.Attributes)
			case "die":
				c.handleDie(msg.Actor.ID, msg.Actor.Attributes)
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

func (c *Controller) handleDie(containerID string, attrs map[string]string) {
	serviceName := attrs["com.docker.compose.service"]
	if serviceName == "" {
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
}

func (c *Controller) handleStart(ctx context.Context, containerID string, attrs map[string]string) {
	serviceName := attrs["com.docker.compose.service"]
	if serviceName == "" {
		return
	}

	log.Printf("container started: %s (service=%s)", containerID[:12], serviceName)

	containers, err := c.docker.ListManagedContainers(ctx)
	if err != nil {
		log.Printf("error listing containers: %v", err)
		return
	}

	var serviceContainers []types.Container
	for _, ctr := range containers {
		if ctr.Labels["com.docker.compose.service"] == serviceName {
			serviceContainers = append(serviceContainers, ctr)
		}
	}

	if len(serviceContainers) < 2 {
		return
	}

	var old, new []types.Container
	images := make(map[string][]types.Container)
	for _, ctr := range serviceContainers {
		images[ctr.ImageID] = append(images[ctr.ImageID], ctr)
	}

	if len(images) < 2 {
		return
	}

	for _, ctrs := range images {
		isOld := true
		for _, ctr := range ctrs {
			if ctr.ID == containerID {
				isOld = false
				break
			}
		}

		if isOld {
			old = ctrs
		} else {
			new = ctrs
		}
	}

	if len(old) == 0 || len(new) == 0 {
		return
	}

	ds, err := c.stateManager.Load(serviceName)
	if err != nil {
		log.Printf("error loading state for %s: %v", serviceName, err)
		return
	}

	if ds.Status == state.StatusInProgress {
		log.Printf("deployment already in progress for %s, skipping", serviceName)
		return
	}

	cfg, err := config.ParseLabels(serviceContainers[0].Labels)
	if err != nil {
		log.Printf("error parsing labels for %s: %v", serviceName, err)
		return
	}

	go c.deploy(ctx, serviceName, cfg, old, new)
}

func (c *Controller) deploy(parentCtx context.Context, serviceName string, cfg *config.ServiceConfig, oldContainers, newContainers []types.Container) {
	c.mu.Lock()
	if cancel, ok := c.deployments[serviceName]; ok {
		cancel()
	}

	ctx, cancel := context.WithCancel(parentCtx)
	c.deployments[serviceName] = cancel
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		delete(c.deployments, serviceName)
		c.mu.Unlock()
		cancel()
	}()

	log.Printf("starting %s deployment for %s", cfg.Strategy, serviceName)

	prov := c.createProvider(cfg)
	strat := c.createStrategy(cfg, prov)

	oldInfos, err := c.resolveContainers(ctx, oldContainers)
	if err != nil {
		log.Printf("error resolving old containers for %s: %v", serviceName, err)
		return
	}

	newInfos, err := c.resolveContainers(ctx, newContainers)
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

	mon := monitor.NewHealthMonitor(c.docker, newIDs, func(containerID, reason string) {
		log.Printf("auto-rollback triggered for %s: %s", serviceName, reason)
		deployCancel()
	})
	mon.SetGracePeriod(cfg.HealthCheckTimeout)

	go mon.Run(deployCtx)

	if err := strat.Execute(deployCtx, d); err != nil {
		log.Printf("deployment failed for %s: %v", serviceName, err)

		log.Printf("initiating rollback for %s", serviceName)
		if rbErr := strat.Rollback(context.Background(), d); rbErr != nil {
			log.Printf("rollback failed for %s: %v", serviceName, rbErr)
		}
		return
	}

	log.Printf("deployment complete for %s", serviceName)
}

func (c *Controller) createProvider(cfg *config.ServiceConfig) provider.Provider {
	switch cfg.Provider {
	case config.ProviderNginx:
		return provider.NewNginx(cfg.NginxConfigDir, c.docker, cfg.NginxContainer)
	case config.ProviderTraefik:
		return provider.NewTraefik(cfg.TraefikConfigDir)
	case config.ProviderNginxProxy:
		return c.getNginxProxyProvider(cfg)
	case config.ProviderNone:
		return provider.NewNoop()
	default:
		return provider.NewNginx(cfg.NginxConfigDir, c.docker, cfg.NginxContainer)
	}
}

func (c *Controller) getNginxProxyProvider(cfg *config.ServiceConfig) provider.Provider {
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

func (c *Controller) createStrategy(cfg *config.ServiceConfig, prov provider.Provider) strategy.Strategy {
	switch cfg.Strategy {
	case config.StrategyBlueGreen:
		return strategy.NewBlueGreen(c.docker, prov, c.stateManager)
	case config.StrategyCanary:
		return strategy.NewCanary(c.docker, prov, c.stateManager)
	default:
		return strategy.NewLinear(c.docker, prov, c.stateManager)
	}
}

func (c *Controller) resolveContainers(ctx context.Context, containers []types.Container) ([]strategy.ContainerInfo, error) {
	var infos []strategy.ContainerInfo

	for _, ctr := range containers {
		addr, err := c.docker.ContainerAddr(ctx, ctr.ID)
		if err != nil {
			log.Printf("warning: resolving %s: %v", ctr.ID[:12], err)
			continue
		}

		infos = append(infos, strategy.ContainerInfo{
			ID:   ctr.ID,
			Addr: addr,
		})
	}

	return infos, nil
}

func (c *Controller) Release(ctx context.Context, service string, force bool) error {
	containers, err := c.docker.ListManagedContainers(ctx)
	if err != nil {
		return fmt.Errorf("listing containers: %w", err)
	}

	var serviceContainers []types.Container
	for _, ctr := range containers {
		if ctr.Labels["com.docker.compose.service"] == service {
			serviceContainers = append(serviceContainers, ctr)
		}
	}

	if len(serviceContainers) == 0 {
		return fmt.Errorf("no managed containers found for service %q", service)
	}

	cfg, err := config.ParseLabels(serviceContainers[0].Labels)
	if err != nil {
		return fmt.Errorf("parsing labels: %w", err)
	}

	images := make(map[string][]types.Container)
	for _, ctr := range serviceContainers {
		images[ctr.ImageID] = append(images[ctr.ImageID], ctr)
	}

	if len(images) >= 2 {
		oldContainers, newContainers := splitByImage(serviceContainers, images)
		log.Printf("releasing %s: %d old → %d new", service, len(oldContainers), len(newContainers))
		c.deploy(ctx, service, cfg, oldContainers, newContainers)
		return nil
	}

	if !force {
		return fmt.Errorf("no pending deployment for %q (all containers share the same image, use --force to redeploy)", service)
	}

	newContainers, err := c.scaleUp(ctx, serviceContainers)
	if err != nil {
		return fmt.Errorf("scaling up: %w", err)
	}

	log.Printf("releasing %s: %d old → %d new", service, len(serviceContainers), len(newContainers))
	c.deploy(ctx, service, cfg, serviceContainers, newContainers)

	return nil
}

func (c *Controller) scaleUp(ctx context.Context, existing []types.Container) ([]types.Container, error) {
	log.Printf("scaling up: creating %d container(s) from image", len(existing))

	var newIDs []string
	for _, ctr := range existing {
		newID, err := c.docker.CreateContainerFromImage(ctx, ctr)
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

	allContainers, err := c.docker.ListManagedContainers(ctx)
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

func splitByImage(containers []types.Container, images map[string][]types.Container) (old, new []types.Container) {
	var newestTime int64
	var newestImage string
	for _, ctr := range containers {
		if ctr.Created > newestTime {
			newestTime = ctr.Created
			newestImage = ctr.ImageID
		}
	}

	for imageID, ctrs := range images {
		if imageID == newestImage {
			new = ctrs
		} else {
			old = append(old, ctrs...)
		}
	}

	return old, new
}

func (c *Controller) Rollback(ctx context.Context, service string) error {
	coord := rollback.NewCoordinator(c.stateManager, c.docker)

	cfg := c.resolveServiceConfig(ctx, service)
	prov := c.createProvider(cfg)
	coord.RegisterStrategy("linear", strategy.NewLinear(c.docker, prov, c.stateManager))
	coord.RegisterStrategy("blue-green", strategy.NewBlueGreen(c.docker, prov, c.stateManager))
	coord.RegisterStrategy("canary", strategy.NewCanary(c.docker, prov, c.stateManager))

	return coord.Execute(ctx, service)
}

func (c *Controller) resolveServiceConfig(ctx context.Context, service string) *config.ServiceConfig {
	containers, err := c.docker.ListManagedContainers(ctx)
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

	fmt.Printf("Service:    %s\n", s.Service)
	fmt.Printf("Status:     %s\n", s.Status)
	fmt.Printf("Strategy:   %s\n", s.Strategy)
	fmt.Printf("Weight:     %d%%\n", s.CurrentWeight)
	fmt.Printf("Stable:     %v\n", s.Containers.Stable)
	fmt.Printf("Canary:     %v\n", s.Containers.Canary)

	return nil
}

func (c *Controller) statusAll(ctx context.Context) error {
	containers, err := c.docker.ListManagedContainers(ctx)
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

		fmt.Printf("%-20s %s\n", name, status)
	}

	return nil
}

func (c *Controller) discoverServices(ctx context.Context) (map[string][]types.Container, error) {
	containers, err := c.docker.ListManagedContainers(ctx)
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
	for name, containers := range services {
		if len(containers) == 0 {
			continue
		}

		cfg, err := config.ParseLabels(containers[0].Labels)
		if err != nil {
			log.Printf("skipping initial config for %s: %v", name, err)
			continue
		}

		c.generateServiceConfig(ctx, name, cfg, containers)
	}
}

func (c *Controller) generateServiceConfig(ctx context.Context, name string, cfg *config.ServiceConfig, containers []types.Container) {
	prov := c.createProvider(cfg)

	upstream := &provider.UpstreamState{
		Service:      name,
		UpstreamName: cfg.UpstreamName,
	}

	for _, ctr := range containers {
		addr, err := c.docker.ContainerAddr(ctx, ctr.ID)
		if err != nil {
			log.Printf("warning: resolving %s: %v", ctr.ID[:12], err)
			continue
		}

		healthy, err := c.docker.IsHealthy(ctx, ctr.ID)
		if err != nil {
			log.Printf("warning: checking health of %s: %v", ctr.ID[:12], err)
			continue
		}

		upstream.Servers = append(upstream.Servers, provider.Server{
			Addr: addr,
			Down: !healthy,
		})
	}

	if len(upstream.Servers) == 0 {
		return
	}

	if err := prov.GenerateConfig(upstream); err != nil {
		log.Printf("error generating config for %s: %v", name, err)
		return
	}

	if err := prov.Reload(); err != nil {
		log.Printf("error reloading provider for %s: %v", name, err)
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

func (c *Controller) refreshServiceConfig(ctx context.Context, serviceName string) {
	c.mu.Lock()
	_, deploying := c.deployments[serviceName]
	c.mu.Unlock()

	if deploying {
		return
	}

	ds, err := c.stateManager.Load(serviceName)
	if err == nil && ds.Status == state.StatusInProgress {
		log.Printf("deployment in progress for %s (from another process), skipping config refresh", serviceName)
		return
	}

	containers, err := c.docker.ListManagedContainers(ctx)
	if err != nil {
		log.Printf("error listing containers: %v", err)
		return
	}

	var serviceContainers []types.Container
	for _, ctr := range containers {
		if ctr.Labels["com.docker.compose.service"] == serviceName {
			serviceContainers = append(serviceContainers, ctr)
		}
	}

	if len(serviceContainers) == 0 {
		return
	}

	cfg, err := config.ParseLabels(serviceContainers[0].Labels)
	if err != nil {
		log.Printf("error parsing labels for %s: %v", serviceName, err)
		return
	}

	c.generateServiceConfig(ctx, serviceName, cfg, serviceContainers)
}

func (c *Controller) handleHealthStatus(ctx context.Context, containerID string, attrs map[string]string) {
	serviceName := attrs["com.docker.compose.service"]
	if serviceName == "" {
		return
	}

	log.Printf("health status changed: %s (service=%s)", containerID[:12], serviceName)

	c.refreshServiceConfig(ctx, serviceName)
}
