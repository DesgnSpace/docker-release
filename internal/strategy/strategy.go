package strategy

import (
	"context"

	"github.com/malico/docker-release/internal/config"
	"github.com/malico/docker-release/internal/provider"
)

type ContainerInfo struct {
	ID   string
	Addr string
}

type Deployment struct {
	Service string
	Config  *config.ServiceConfig
	Old     []ContainerInfo
	New     []ContainerInfo
}

func (d *Deployment) UpstreamName() string {
	if d.Config != nil && d.Config.UpstreamName != "" {
		return d.Config.UpstreamName
	}
	return d.Service
}

func applyNginxKeepalive(d *Deployment, upstream *provider.UpstreamState) {
	if d == nil || d.Config == nil || upstream == nil {
		return
	}

	if d.Config.Provider == config.ProviderNginx || d.Config.Provider == config.ProviderNginxProxy {
		upstream.Keepalive = d.Config.ResolveNginxKeepalive(len(upstream.Servers))
		return
	}

	if d.Config.Provider == config.ProviderAngie {
		upstream.Keepalive = d.Config.ResolveAngieKeepalive(len(upstream.Servers))
		return
	}
}

type Strategy interface {
	Execute(ctx context.Context, d *Deployment) error
	Rollback(ctx context.Context, d *Deployment) error
}
