package strategy

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/malico/docker-release/internal/provider"
	"github.com/malico/docker-release/internal/state"
)

type Canary struct {
	docker   DockerOps
	provider provider.Provider
	state    *state.Manager
}

func NewCanary(docker DockerOps, prov provider.Provider, stateMgr *state.Manager) *Canary {
	return &Canary{
		docker:   docker,
		provider: prov,
		state:    stateMgr,
	}
}

func (c *Canary) Execute(ctx context.Context, d *Deployment) error {
	log.Printf("[canary] starting deployment for %s: %d stable, %d canary", d.Service, len(d.Old), len(d.New))

	prev, _ := c.state.Load(d.Service)
	prevDeployID := ""
	if prev != nil {
		prevDeployID = prev.ActiveDeploymentID
	}

	ds := &state.DeploymentState{
		Service:              d.Service,
		Status:               state.StatusInProgress,
		Strategy:             "canary",
		ActiveDeploymentID:   state.GenerateDeploymentID(),
		PreviousDeploymentID: prevDeployID,
		Containers: state.Containers{
			Stable: containerIDs(d.Old),
			Canary: containerIDs(d.New),
		},
	}

	if err := c.state.Save(ds); err != nil {
		return fmt.Errorf("saving initial state: %w", err)
	}

	for _, cn := range d.New {
		log.Printf("[canary] waiting for %s to be healthy", cn.ID[:12])
		if err := c.docker.WaitHealthy(ctx, cn.ID, d.Config.HealthCheckTimeout); err != nil {
			return fmt.Errorf("health check failed for %s: %w", cn.ID[:12], err)
		}
	}

	canaryCfg := d.Config.Canary
	weight := canaryCfg.StartPercentage

	for weight < 100 {
		if err := ctx.Err(); err != nil {
			return err
		}

		log.Printf("[canary] setting canary weight to %d%%", weight)

		upstream := buildCanaryUpstream(d, weight)
		if err := c.provider.GenerateConfig(upstream); err != nil {
			return fmt.Errorf("generating config at %d%%: %w", weight, err)
		}

		if err := c.provider.Reload(); err != nil {
			return fmt.Errorf("reloading at %d%%: %w", weight, err)
		}

		ds.CurrentWeight = weight
		if err := c.state.Save(ds); err != nil {
			return fmt.Errorf("saving state at %d%%: %w", weight, err)
		}

		log.Printf("[canary] observing for %s at %d%%", canaryCfg.Interval, weight)

		select {
		case <-time.After(canaryCfg.Interval):
		case <-ctx.Done():
			return ctx.Err()
		}

		weight += canaryCfg.Step
		if weight > 100 {
			weight = 100
		}
	}

	log.Printf("[canary] promoting canary to 100%%")

	finalUpstream := &provider.UpstreamState{Service: d.Service, UpstreamName: d.UpstreamName(), Affinity: d.Config.Affinity}
	for _, cn := range d.New {
		finalUpstream.Servers = append(finalUpstream.Servers, provider.Server{Addr: cn.Addr})
	}
	applyNginxKeepalive(d, finalUpstream)

	if err := c.provider.GenerateConfig(finalUpstream); err != nil {
		return fmt.Errorf("generating final deployment config: %w", err)
	}

	if err := c.provider.Reload(); err != nil {
		return fmt.Errorf("reloading final deployment: %w", err)
	}

	log.Printf("[canary] draining old containers for %s", d.Config.DrainTimeout)

	select {
	case <-time.After(d.Config.DrainTimeout):
	case <-ctx.Done():
		return ctx.Err()
	}

	stableUpstream := &provider.UpstreamState{Service: d.Service, UpstreamName: d.UpstreamName()}
	for _, cn := range d.New {
		stableUpstream.Servers = append(stableUpstream.Servers, provider.Server{Addr: cn.Addr})
	}
	applyNginxKeepalive(d, stableUpstream)

	if err := c.provider.GenerateConfig(stableUpstream); err != nil {
		return fmt.Errorf("generating final stable config: %w", err)
	}

	if err := c.provider.Reload(); err != nil {
		return fmt.Errorf("reloading final stable: %w", err)
	}

	for _, old := range d.Old {
		if err := c.docker.Stop(ctx, old.ID, 10); err != nil {
			log.Printf("[canary] warning: stop %s: %v", old.ID[:12], err)
		}

		if err := c.docker.Remove(ctx, old.ID); err != nil {
			log.Printf("[canary] warning: remove %s: %v", old.ID[:12], err)
		}
	}

	ds.Status = state.StatusIdle
	ds.CurrentWeight = 100
	ds.Containers.Stable = containerIDs(d.New)
	ds.Containers.Canary = nil
	if err := c.state.Save(ds); err != nil {
		return fmt.Errorf("saving final state: %w", err)
	}

	log.Printf("[canary] deployment complete for %s", d.Service)
	return nil
}

func (c *Canary) Rollback(ctx context.Context, d *Deployment) error {
	log.Printf("[canary] rolling back %s", d.Service)

	upstream := &provider.UpstreamState{Service: d.Service, UpstreamName: d.UpstreamName()}
	for _, old := range d.Old {
		upstream.Servers = append(upstream.Servers, provider.Server{Addr: old.Addr})
	}
	applyNginxKeepalive(d, upstream)

	if err := c.provider.GenerateConfig(upstream); err != nil {
		return fmt.Errorf("generating rollback config: %w", err)
	}

	if err := c.provider.Reload(); err != nil {
		return fmt.Errorf("reloading provider: %w", err)
	}

	for _, cn := range d.New {
		if err := c.docker.Stop(ctx, cn.ID, 10); err != nil {
			log.Printf("[canary] warning: stop %s: %v", cn.ID[:12], err)
		}

		if err := c.docker.Remove(ctx, cn.ID); err != nil {
			log.Printf("[canary] warning: remove %s: %v", cn.ID[:12], err)
		}
	}

	ds := &state.DeploymentState{
		Service:    d.Service,
		Status:     state.StatusIdle,
		Strategy:   "canary",
		Containers: state.Containers{Stable: containerIDs(d.Old)},
	}

	if err := c.state.Save(ds); err != nil {
		return fmt.Errorf("saving rollback state: %w", err)
	}

	log.Printf("[canary] rollback complete for %s", d.Service)
	return nil
}

func buildCanaryUpstream(d *Deployment, canaryWeight int) *provider.UpstreamState {
	stableWeight := 100 - canaryWeight

	upstream := &provider.UpstreamState{
		Service:      d.Service,
		UpstreamName: d.UpstreamName(),
		Affinity:     d.Config.Affinity,
	}

	for _, old := range d.Old {
		upstream.Servers = append(upstream.Servers, provider.Server{
			Addr:   old.Addr,
			Weight: stableWeight,
			Group:  "stable",
		})
	}

	for _, cn := range d.New {
		upstream.Servers = append(upstream.Servers, provider.Server{
			Addr:   cn.Addr,
			Weight: canaryWeight,
			Group:  "canary",
		})
	}
	applyNginxKeepalive(d, upstream)

	return upstream
}
