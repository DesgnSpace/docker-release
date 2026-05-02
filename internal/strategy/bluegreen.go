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

	prev, _ := bg.state.Load(d.Service)
	prevDeployID := ""
	if prev != nil {
		prevDeployID = prev.ActiveDeploymentID
	}

	ds := &state.DeploymentState{
		Service:              d.Service,
		Status:               state.StatusInProgress,
		Strategy:             "blue-green",
		ActiveDeploymentID:   state.GenerateDeploymentID(),
		PreviousDeploymentID: prevDeployID,
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

	log.Printf("[blue-green] all green containers healthy, cutting over traffic")

	cutoverUpstream := buildBlueGreenCutoverUpstream(d)
	if err := bg.provider.GenerateConfig(cutoverUpstream); err != nil {
		return fmt.Errorf("generating cutover config: %w", err)
	}

	if err := bg.provider.Reload(); err != nil {
		return fmt.Errorf("reloading provider: %w", err)
	}

	ds.CurrentWeight = d.Config.BlueGreen.GreenWeight
	if err := bg.state.Save(ds); err != nil {
		return fmt.Errorf("saving cutover state: %w", err)
	}

	soakTime := d.Config.BlueGreen.SoakTime
	log.Printf("[blue-green] soaking on green for %s before removing blue", soakTime)

	select {
	case <-time.After(soakTime):
	case <-ctx.Done():
		return ctx.Err()
	}

	finalUpstream := &provider.UpstreamState{Service: d.Service, UpstreamName: d.UpstreamName()}
	for _, c := range d.New {
		finalUpstream.Servers = append(finalUpstream.Servers, provider.Server{Addr: c.Addr})
	}
	applyNginxKeepalive(d, finalUpstream)

	if err := bg.provider.GenerateConfig(finalUpstream); err != nil {
		return fmt.Errorf("generating green config: %w", err)
	}

	if err := bg.provider.Reload(); err != nil {
		return fmt.Errorf("reloading provider: %w", err)
	}

	log.Printf("[blue-green] draining blue containers for %s", d.Config.DrainTimeout)

	select {
	case <-time.After(d.Config.DrainTimeout):
	case <-ctx.Done():
		return ctx.Err()
	}

	log.Printf("[blue-green] tearing down blue containers")

	for _, c := range d.Old {
		if err := bg.docker.Stop(ctx, c.ID, 10); err != nil {
			log.Printf("[blue-green] warning: stop %s: %v", c.ID[:12], err)
		}

		if err := bg.docker.Remove(ctx, c.ID); err != nil {
			log.Printf("[blue-green] warning: remove %s: %v", c.ID[:12], err)
		}
	}

	ds.Status = state.StatusIdle
	ds.CurrentWeight = 100
	ds.Containers.Stable = containerIDs(d.New)
	ds.Containers.Canary = nil
	if err := bg.state.Save(ds); err != nil {
		return fmt.Errorf("saving final state: %w", err)
	}

	log.Printf("[blue-green] deployment complete for %s", d.Service)
	return nil
}

func buildBlueGreenCutoverUpstream(d *Deployment) *provider.UpstreamState {
	greenWeight := d.Config.BlueGreen.GreenWeight
	blueWeight := 100 - greenWeight

	upstream := &provider.UpstreamState{
		Service:      d.Service,
		UpstreamName: d.UpstreamName(),
		Affinity:     d.Config.BlueGreen.Affinity,
	}

	for _, c := range d.Old {
		upstream.Servers = append(upstream.Servers, provider.Server{Addr: c.Addr, Weight: blueWeight, Group: "stable"})
	}

	for _, c := range d.New {
		upstream.Servers = append(upstream.Servers, provider.Server{Addr: c.Addr, Weight: greenWeight, Group: "canary"})
	}

	applyNginxKeepalive(d, upstream)

	return upstream
}

func (bg *BlueGreen) Rollback(ctx context.Context, d *Deployment) error {
	log.Printf("[blue-green] rolling back %s to blue", d.Service)

	upstream := &provider.UpstreamState{Service: d.Service, UpstreamName: d.UpstreamName()}
	for _, c := range d.Old {
		upstream.Servers = append(upstream.Servers, provider.Server{Addr: c.Addr})
	}
	applyNginxKeepalive(d, upstream)

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
