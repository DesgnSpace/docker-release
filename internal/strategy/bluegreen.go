package strategy

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/malico/docker-release/internal/provider"
	"github.com/malico/docker-release/internal/state"
)

type BlueGreen struct {
	docker   DockerOps
	provider provider.Provider
	state    *state.Manager
}

func NewBlueGreen(docker DockerOps, prov provider.Provider, stateMgr *state.Manager) *BlueGreen {
	return &BlueGreen{
		docker:   docker,
		provider: prov,
		state:    stateMgr,
	}
}

func (bg *BlueGreen) Execute(ctx context.Context, d *Deployment) error {
	log.Printf("[blue-green] starting deployment for %s: %d old (blue) → %d new (green)", d.Service, len(d.Old), len(d.New))

	ds := &state.DeploymentState{
		Service:  d.Service,
		Status:   state.StatusInProgress,
		Strategy: "blue-green",
		Containers: state.Containers{
			Stable: containerIDs(d.Old),
			Canary: containerIDs(d.New),
		},
	}

	if err := bg.state.Save(ds); err != nil {
		return fmt.Errorf("saving initial state: %w", err)
	}

	for _, c := range d.New {
		log.Printf("[blue-green] waiting for %s to be healthy", c.ID[:12])
		if err := bg.docker.WaitHealthy(ctx, c.ID, d.Config.HealthCheckTimeout); err != nil {
			return fmt.Errorf("health check failed for %s: %w", c.ID[:12], err)
		}
	}

	log.Printf("[blue-green] all green containers healthy, switching traffic")

	upstream := &provider.UpstreamState{Service: d.Service}
	for _, c := range d.New {
		upstream.Servers = append(upstream.Servers, provider.Server{Addr: c.Addr})
	}

	if err := bg.provider.GenerateConfig(upstream); err != nil {
		return fmt.Errorf("generating green config: %w", err)
	}

	if err := bg.provider.Reload(); err != nil {
		return fmt.Errorf("reloading provider: %w", err)
	}

	ds.CurrentWeight = 100
	if err := bg.state.Save(ds); err != nil {
		return fmt.Errorf("saving cutover state: %w", err)
	}

	soakTime := d.Config.BlueGreen.SoakTime
	log.Printf("[blue-green] soaking for %s", soakTime)

	select {
	case <-time.After(soakTime):
	case <-ctx.Done():
		return ctx.Err()
	}

	log.Printf("[blue-green] soak complete, tearing down blue containers")

	for _, c := range d.Old {
		if err := bg.docker.Stop(ctx, c.ID, 10); err != nil {
			log.Printf("[blue-green] warning: stop %s: %v", c.ID[:12], err)
		}

		if err := bg.docker.Remove(ctx, c.ID); err != nil {
			log.Printf("[blue-green] warning: remove %s: %v", c.ID[:12], err)
		}
	}

	ds.Status = state.StatusIdle
	ds.Containers.Stable = containerIDs(d.New)
	ds.Containers.Canary = nil
	if err := bg.state.Save(ds); err != nil {
		return fmt.Errorf("saving final state: %w", err)
	}

	log.Printf("[blue-green] deployment complete for %s", d.Service)
	return nil
}

func (bg *BlueGreen) Rollback(ctx context.Context, d *Deployment) error {
	log.Printf("[blue-green] rolling back %s to blue", d.Service)

	upstream := &provider.UpstreamState{Service: d.Service}
	for _, c := range d.Old {
		upstream.Servers = append(upstream.Servers, provider.Server{Addr: c.Addr})
	}

	if err := bg.provider.GenerateConfig(upstream); err != nil {
		return fmt.Errorf("generating rollback config: %w", err)
	}

	if err := bg.provider.Reload(); err != nil {
		return fmt.Errorf("reloading provider: %w", err)
	}

	for _, c := range d.New {
		if err := bg.docker.Stop(ctx, c.ID, 10); err != nil {
			log.Printf("[blue-green] warning: stop %s: %v", c.ID[:12], err)
		}

		if err := bg.docker.Remove(ctx, c.ID); err != nil {
			log.Printf("[blue-green] warning: remove %s: %v", c.ID[:12], err)
		}
	}

	ds := &state.DeploymentState{
		Service:    d.Service,
		Status:     state.StatusIdle,
		Strategy:   "blue-green",
		Containers: state.Containers{Stable: containerIDs(d.Old)},
	}

	if err := bg.state.Save(ds); err != nil {
		return fmt.Errorf("saving rollback state: %w", err)
	}

	log.Printf("[blue-green] rollback complete for %s", d.Service)
	return nil
}
