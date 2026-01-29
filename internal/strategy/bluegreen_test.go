package strategy

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/malico/docker-release/internal/config"
	"github.com/malico/docker-release/internal/state"
)

func bgDeployment() *Deployment {
	return &Deployment{
		Service: "app",
		Config: &config.ServiceConfig{
			HealthCheckTimeout: time.Second,
			DrainTimeout:       time.Millisecond,
			BlueGreen: config.BlueGreenConfig{
				SoakTime: time.Millisecond,
			},
		},
		Old: []ContainerInfo{
			{ID: "blue_0_xxxxxxx_full", Addr: "172.18.0.2:80"},
			{ID: "blue_1_xxxxxxx_full", Addr: "172.18.0.3:80"},
		},
		New: []ContainerInfo{
			{ID: "green_0_xxxxxx_full", Addr: "172.18.0.10:80"},
			{ID: "green_1_xxxxxx_full", Addr: "172.18.0.11:80"},
		},
	}
}

func TestBlueGreenExecute(t *testing.T) {
	docker := &mockDocker{}
	prov := &mockProvider{}
	stateMgr := state.NewManager(t.TempDir())

	bg := NewBlueGreen(docker, prov, stateMgr)

	if err := bg.Execute(context.Background(), bgDeployment()); err != nil {
		t.Fatalf("execute: %v", err)
	}

	if len(docker.healthyCalls) != 2 {
		t.Errorf("expected 2 health checks, got %d", len(docker.healthyCalls))
	}

	if len(prov.configs) != 1 {
		t.Fatalf("expected 1 config generation, got %d", len(prov.configs))
	}

	cfg := prov.configs[0]
	if len(cfg.Servers) != 2 {
		t.Errorf("expected 2 servers (green only), got %d", len(cfg.Servers))
	}

	for _, s := range cfg.Servers {
		if !strings.Contains(s.Addr, "172.18.0.1") {
			t.Errorf("config should only have green servers, got %s", s.Addr)
		}
	}

	if prov.reloads != 1 {
		t.Errorf("expected 1 reload, got %d", prov.reloads)
	}

	if len(docker.stopCalls) != 2 {
		t.Errorf("expected 2 stops (blue teardown), got %d", len(docker.stopCalls))
	}

	for _, id := range docker.stopCalls {
		if !strings.HasPrefix(id, "blue_") {
			t.Errorf("should stop blue containers, got %s", id)
		}
	}
}

func TestBlueGreenSavesState(t *testing.T) {
	docker := &mockDocker{}
	prov := &mockProvider{}
	dir := t.TempDir()
	stateMgr := state.NewManager(dir)

	bg := NewBlueGreen(docker, prov, stateMgr)

	if err := bg.Execute(context.Background(), bgDeployment()); err != nil {
		t.Fatalf("execute: %v", err)
	}

	s, err := stateMgr.Load("app")
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if s.Status != state.StatusIdle {
		t.Errorf("expected idle, got %s", s.Status)
	}

	if len(s.Containers.Stable) != 2 {
		t.Errorf("expected 2 stable (green), got %d", len(s.Containers.Stable))
	}

	if len(s.Containers.Canary) != 0 {
		t.Errorf("expected 0 canary, got %d", len(s.Containers.Canary))
	}
}

func TestBlueGreenHealthFailure(t *testing.T) {
	docker := &mockDocker{healthErr: fmt.Errorf("unhealthy")}
	prov := &mockProvider{}
	stateMgr := state.NewManager(t.TempDir())

	bg := NewBlueGreen(docker, prov, stateMgr)

	err := bg.Execute(context.Background(), bgDeployment())
	if err == nil {
		t.Fatal("expected error")
	}

	if !strings.Contains(err.Error(), "health check failed") {
		t.Errorf("expected health check error, got: %v", err)
	}

	if len(prov.configs) != 0 {
		t.Error("should not generate config on failure")
	}

	if len(docker.stopCalls) != 0 {
		t.Error("should not stop anything on failure")
	}
}

func TestBlueGreenRollback(t *testing.T) {
	docker := &mockDocker{}
	prov := &mockProvider{}
	stateMgr := state.NewManager(t.TempDir())

	bg := NewBlueGreen(docker, prov, stateMgr)
	d := bgDeployment()

	if err := bg.Rollback(context.Background(), d); err != nil {
		t.Fatalf("rollback: %v", err)
	}

	if len(prov.configs) != 1 {
		t.Fatalf("expected 1 config, got %d", len(prov.configs))
	}

	cfg := prov.configs[0]
	for _, s := range cfg.Servers {
		if strings.Contains(s.Addr, "172.18.0.1") {
			t.Error("rollback should only have blue servers")
		}
	}

	if len(docker.stopCalls) != 2 {
		t.Errorf("expected 2 stops (green), got %d", len(docker.stopCalls))
	}

	for _, id := range docker.stopCalls {
		if !strings.HasPrefix(id, "green_") {
			t.Errorf("should stop green containers, got %s", id)
		}
	}

	s, err := stateMgr.Load("app")
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if s.Status != state.StatusIdle {
		t.Errorf("expected idle, got %s", s.Status)
	}
}

func TestBlueGreenContextCancel(t *testing.T) {
	docker := &mockDocker{}
	prov := &mockProvider{}
	stateMgr := state.NewManager(t.TempDir())

	bg := NewBlueGreen(docker, prov, stateMgr)
	d := bgDeployment()
	d.Config.BlueGreen.SoakTime = 5 * time.Second

	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()

	err := bg.Execute(ctx, d)
	if err == nil {
		t.Fatal("expected context error")
	}
}
