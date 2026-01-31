package strategy

import (
	"context"

	"github.com/malico/docker-release/internal/config"
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

type Strategy interface {
	Execute(ctx context.Context, d *Deployment) error
	Rollback(ctx context.Context, d *Deployment) error
}
