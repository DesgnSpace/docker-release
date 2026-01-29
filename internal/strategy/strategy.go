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

type Strategy interface {
	Execute(ctx context.Context, d *Deployment) error
	Rollback(ctx context.Context, d *Deployment) error
}
