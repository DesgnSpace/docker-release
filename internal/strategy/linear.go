package strategy

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/malico/docker-release/internal/provider"
	"github.com/malico/docker-release/internal/state"
)

type DockerOps interface {
	WaitHealthy(ctx context.Context, containerID string, timeout time.Duration) error
	Stop(ctx context.Context, containerID string, timeoutSeconds int) error
	Remove(ctx context.Context, containerID string) error
}

type Linear struct {
	docker   DockerOps
	provider provider.Provider
	state    *state.Manager
}

func NewLinear(docker DockerOps, prov provider.Provider, stateMgr *state.Manager) *Linear {
	return &Linear{
		docker:   docker,
		provider: prov,
		state:    stateMgr,
	}
}

func (l *Linear) Execute(ctx context.Context, d *Deployment) error {
	log.Printf("[linear] starting deployment for %s: %d old → %d new", d.Service, len(d.Old), len(d.New))

	prev, _ := l.state.Load(d.Service)
	prevDeployID := ""
	if prev != nil {
		prevDeployID = prev.ActiveDeploymentID
	}

	ds := &state.DeploymentState{
		Service:              d.Service,
		Status:               state.StatusInProgress,
		Strategy:             "linear",
		ActiveDeploymentID:   state.GenerateDeploymentID(),
		PreviousDeploymentID: prevDeployID,
		Containers: state.Containers{
			Stable: containerIDs(d.Old),
		},
	}

	if err := l.state.Save(ds); err != nil {
		return fmt.Errorf("saving initial state: %w", err)
	}

	replacements := min(len(d.Old), len(d.New))
	for i := 0; i < replacements; i++ {
		if err := ctx.Err(); err != nil {
			return err
		}

		oldC := d.Old[i]
		newC := d.New[i]

		log.Printf("[linear] step %d/%d: replacing %s with %s", i+1, replacements, oldC.ID[:12], newC.ID[:12])

		log.Printf("[linear] waiting for %s to be healthy", newC.ID[:12])
		if err := l.docker.WaitHealthy(ctx, newC.ID, d.Config.HealthCheckTimeout); err != nil {
			return fmt.Errorf("health check failed for %s: %w", newC.ID[:12], err)
		}

		upstream := l.buildUpstream(d, i)
		applyNginxKeepalive(d, upstream)
		if err := l.provider.GenerateConfig(upstream); err != nil {
			return fmt.Errorf("generating config: %w", err)
		}

		if err := l.provider.Reload(); err != nil {
			return fmt.Errorf("reloading provider: %w", err)
		}

		log.Printf("[linear] draining %s for %s", oldC.ID[:12], d.Config.DrainTimeout)
		select {
		case <-time.After(d.Config.DrainTimeout):
		case <-ctx.Done():
			return ctx.Err()
		}

		log.Printf("[linear] stopping %s", oldC.ID[:12])
		if err := l.docker.Stop(ctx, oldC.ID, 10); err != nil {
			log.Printf("[linear] warning: stop %s: %v", oldC.ID[:12], err)
		}

		if err := l.docker.Remove(ctx, oldC.ID); err != nil {
			log.Printf("[linear] warning: remove %s: %v", oldC.ID[:12], err)
		}

		ds.Containers.Stable = containerIDs(d.New[:i+1])
		if i+1 < replacements {
			ds.Containers.Stable = append(ds.Containers.Stable, containerIDs(d.Old[i+1:])...)
		}
		if err := l.state.Save(ds); err != nil {
			return fmt.Errorf("saving state at step %d: %w", i+1, err)
		}
	}

	if len(d.New) > len(d.Old) {
		for i := len(d.Old); i < len(d.New); i++ {
			log.Printf("[linear] waiting for extra container %s to be healthy", d.New[i].ID[:12])
			if err := l.docker.WaitHealthy(ctx, d.New[i].ID, d.Config.HealthCheckTimeout); err != nil {
				return fmt.Errorf("health check failed for %s: %w", d.New[i].ID[:12], err)
			}
		}
	}

	upstream := l.buildFinalUpstream(d)
	applyNginxKeepalive(d, upstream)
	if err := l.provider.GenerateConfig(upstream); err != nil {
		return fmt.Errorf("generating final config: %w", err)
	}

	if err := l.provider.Reload(); err != nil {
		return fmt.Errorf("reloading provider: %w", err)
	}

	ds.Status = state.StatusIdle
	ds.Containers.Stable = containerIDs(d.New)
	ds.Containers.Canary = nil
	if err := l.state.Save(ds); err != nil {
		return fmt.Errorf("saving final state: %w", err)
	}

	log.Printf("[linear] deployment complete for %s", d.Service)
	return nil
}

func (l *Linear) Rollback(ctx context.Context, d *Deployment) error {
	log.Printf("[linear] rolling back %s", d.Service)

	upstream := &provider.UpstreamState{
		Service:      d.Service,
		UpstreamName: d.UpstreamName(),
	}

	for _, c := range d.Old {
		upstream.Servers = append(upstream.Servers, provider.Server{Addr: c.Addr})
	}
	applyNginxKeepalive(d, upstream)

	if err := l.provider.GenerateConfig(upstream); err != nil {
		return fmt.Errorf("generating rollback config: %w", err)
	}

	if err := l.provider.Reload(); err != nil {
		return fmt.Errorf("reloading provider: %w", err)
	}

	for _, c := range d.New {
		if err := l.docker.Stop(ctx, c.ID, 10); err != nil {
			log.Printf("[linear] warning: stop %s: %v", c.ID[:12], err)
		}

		if err := l.docker.Remove(ctx, c.ID); err != nil {
			log.Printf("[linear] warning: remove %s: %v", c.ID[:12], err)
		}
	}

	ds := &state.DeploymentState{
		Service:    d.Service,
		Status:     state.StatusIdle,
		Strategy:   "linear",
		Containers: state.Containers{Stable: containerIDs(d.Old)},
	}

	if err := l.state.Save(ds); err != nil {
		return fmt.Errorf("saving rollback state: %w", err)
	}

	log.Printf("[linear] rollback complete for %s", d.Service)
	return nil
}

func (l *Linear) buildUpstream(d *Deployment, step int) *provider.UpstreamState {
	upstream := &provider.UpstreamState{Service: d.Service, UpstreamName: d.UpstreamName()}

	for j := 0; j <= step; j++ {
		upstream.Servers = append(upstream.Servers, provider.Server{Addr: d.New[j].Addr})
	}

	upstream.Servers = append(upstream.Servers, provider.Server{
		Addr: d.Old[step].Addr,
		Down: true,
	})

	for j := step + 1; j < len(d.Old); j++ {
		upstream.Servers = append(upstream.Servers, provider.Server{Addr: d.Old[j].Addr})
	}

	return upstream
}

func (l *Linear) buildFinalUpstream(d *Deployment) *provider.UpstreamState {
	upstream := &provider.UpstreamState{Service: d.Service, UpstreamName: d.UpstreamName()}

	for _, c := range d.New {
		upstream.Servers = append(upstream.Servers, provider.Server{Addr: c.Addr})
	}

	return upstream
}

func containerIDs(containers []ContainerInfo) []string {
	ids := make([]string, len(containers))
	for i, c := range containers {
		ids[i] = c.ID
	}
	return ids
}
