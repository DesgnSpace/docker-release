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
	configDir    string

	mu          sync.Mutex
	deployments map[string]context.CancelFunc
}

func New(dockerClient *docker.Client, stateManager *state.Manager, configDir string) *Controller {
	return &Controller{
		docker:       dockerClient,
		stateManager: stateManager,
		configDir:    configDir,
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

	log.Println("watching for events... (ctrl+c to stop)")

	msgCh, errCh := c.docker.Events(ctx)
	for {
		select {
		case msg := <-msgCh:
			if msg.Action == "start" {
				c.handleStart(ctx, msg.Actor.ID, msg.Actor.Attributes)
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

	var oldImage string
	for img, ctrs := range images {
		isOld := true
		for _, ctr := range ctrs {
			if ctr.ID == containerID {
				isOld = false
				break
			}
		}

		if isOld {
			oldImage = img
			old = ctrs
		} else {
			new = ctrs
		}
	}

	_ = oldImage

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

	monCtx, monCancel := context.WithCancel(ctx)
	defer monCancel()

	newIDs := make([]string, len(newInfos))
	for i, info := range newInfos {
		newIDs[i] = info.ID
	}

	mon := monitor.NewHealthMonitor(c.docker, newIDs, func(containerID, reason string) {
		log.Printf("auto-rollback triggered for %s: %s", serviceName, reason)
		monCancel()
	})

	go mon.Run(monCtx)

	if err := strat.Execute(ctx, d); err != nil {
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
		return provider.NewNginx(c.configDir, c.docker, "")
	case config.ProviderTraefik:
		return provider.NewTraefik(c.configDir)
	default:
		return provider.NewNginx(c.configDir, c.docker, "")
	}
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

func (c *Controller) Rollback(ctx context.Context, service string) error {
	coord := rollback.NewCoordinator(c.stateManager, c.docker)

	prov := c.createProvider(&config.ServiceConfig{Provider: config.ProviderNginx})
	coord.RegisterStrategy("linear", strategy.NewLinear(c.docker, prov, c.stateManager))
	coord.RegisterStrategy("blue-green", strategy.NewBlueGreen(c.docker, prov, c.stateManager))
	coord.RegisterStrategy("canary", strategy.NewCanary(c.docker, prov, c.stateManager))

	return coord.Execute(ctx, service)
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
