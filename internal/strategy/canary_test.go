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

func canaryDeployment() *Deployment {
	return &Deployment{
		Service: "app",
		Config: &config.ServiceConfig{
			HealthCheckTimeout: time.Second,
			DrainTimeout:       time.Millisecond,
			Canary: config.CanaryConfig{
				StartPercentage: 25,
				Step:            25,
				Interval:        time.Millisecond,
				Affinity:        "ip",
			},
		},
		Old: []ContainerInfo{
			{ID: "stable_0_xxxxx_full", Addr: "172.18.0.2:80"},
			{ID: "stable_1_xxxxx_full", Addr: "172.18.0.3:80"},
		},
		New: []ContainerInfo{
			{ID: "canary_0_xxxxx_full", Addr: "172.18.0.10:80"},
		},
	}
}

func TestCanaryExecute(t *testing.T) {
	docker := &mockDocker{}
	prov := &mockProvider{}
	stateMgr := state.NewManager(t.TempDir())

	c := NewCanary(docker, prov, stateMgr)
	d := canaryDeployment()

	if err := c.Execute(context.Background(), d); err != nil {
		t.Fatalf("execute: %v", err)
	}

	if len(docker.healthyCalls) != 1 {
		t.Errorf("expected 1 health check, got %d", len(docker.healthyCalls))
	}

	// 25% → 50% → 75% → exits loop at 100%, then final config
	// Steps: 25, 50, 75 (3 weighted configs) + 1 final = 4 total
	if len(prov.configs) != 4 {
		t.Errorf("expected 4 config generations (3 canary steps + final), got %d", len(prov.configs))
	}

	if prov.reloads != 4 {
		t.Errorf("expected 4 reloads, got %d", prov.reloads)
	}

	if len(docker.stopCalls) != 2 {
		t.Errorf("expected 2 stops (old containers), got %d", len(docker.stopCalls))
	}

	for _, id := range docker.stopCalls {
		if !strings.HasPrefix(id, "stable_") {
			t.Errorf("should stop stable containers, got %s", id)
		}
	}
}

func TestCanaryWeightProgression(t *testing.T) {
	docker := &mockDocker{}
	prov := &mockProvider{}
	stateMgr := state.NewManager(t.TempDir())

	c := NewCanary(docker, prov, stateMgr)
	d := canaryDeployment()

	if err := c.Execute(context.Background(), d); err != nil {
		t.Fatalf("execute: %v", err)
	}

	expectedWeights := []int{25, 50, 75}
	for i, w := range expectedWeights {
		cfg := prov.configs[i]

		if cfg.Affinity != "ip" {
			t.Errorf("step %d: expected ip affinity, got %s", i, cfg.Affinity)
		}

		foundCanaryWeight := false
		foundStableWeight := false
		for _, s := range cfg.Servers {
			if s.Addr == "172.18.0.10:80" && s.Weight == w {
				foundCanaryWeight = true
			}
			if s.Addr == "172.18.0.2:80" && s.Weight == (100-w) {
				foundStableWeight = true
			}
		}

		if !foundCanaryWeight {
			t.Errorf("step %d: missing canary server with weight %d", i, w)
		}
		if !foundStableWeight {
			t.Errorf("step %d: missing stable server with weight %d", i, 100-w)
		}
	}

	finalCfg := prov.configs[3]
	if len(finalCfg.Servers) != 1 {
		t.Errorf("final config should have 1 server (canary only), got %d", len(finalCfg.Servers))
	}

	if finalCfg.Servers[0].Weight != 0 {
		t.Errorf("final config should have no weight (equal distribution), got %d", finalCfg.Servers[0].Weight)
	}
}

func TestCanarySavesState(t *testing.T) {
	docker := &mockDocker{}
	prov := &mockProvider{}
	dir := t.TempDir()
	stateMgr := state.NewManager(dir)

	c := NewCanary(docker, prov, stateMgr)

	if err := c.Execute(context.Background(), canaryDeployment()); err != nil {
		t.Fatalf("execute: %v", err)
	}

	s, err := stateMgr.Load("app")
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if s.Status != state.StatusIdle {
		t.Errorf("expected idle, got %s", s.Status)
	}

	if s.CurrentWeight != 100 {
		t.Errorf("expected weight 100, got %d", s.CurrentWeight)
	}

	if len(s.Containers.Stable) != 1 {
		t.Errorf("expected 1 stable (promoted canary), got %d", len(s.Containers.Stable))
	}

	if len(s.Containers.Canary) != 0 {
		t.Errorf("expected 0 canary, got %d", len(s.Containers.Canary))
	}
}

func TestCanaryHealthFailure(t *testing.T) {
	docker := &mockDocker{healthErr: fmt.Errorf("unhealthy")}
	prov := &mockProvider{}
	stateMgr := state.NewManager(t.TempDir())

	c := NewCanary(docker, prov, stateMgr)

	err := c.Execute(context.Background(), canaryDeployment())
	if err == nil {
		t.Fatal("expected error")
	}

	if !strings.Contains(err.Error(), "health check failed") {
		t.Errorf("expected health check error, got: %v", err)
	}

	if len(prov.configs) != 0 {
		t.Error("should not generate config on failure")
	}
}

func TestCanaryRollback(t *testing.T) {
	docker := &mockDocker{}
	prov := &mockProvider{}
	stateMgr := state.NewManager(t.TempDir())

	c := NewCanary(docker, prov, stateMgr)
	d := canaryDeployment()

	if err := c.Rollback(context.Background(), d); err != nil {
		t.Fatalf("rollback: %v", err)
	}

	if len(prov.configs) != 1 {
		t.Fatalf("expected 1 config, got %d", len(prov.configs))
	}

	cfg := prov.configs[0]
	if len(cfg.Servers) != 2 {
		t.Errorf("expected 2 servers (stable only), got %d", len(cfg.Servers))
	}

	for _, s := range cfg.Servers {
		if s.Weight != 0 {
			t.Errorf("rollback should have no weights, got %d", s.Weight)
		}
	}

	if len(docker.stopCalls) != 1 {
		t.Errorf("expected 1 stop (canary), got %d", len(docker.stopCalls))
	}

	if docker.stopCalls[0] != "canary_0_xxxxx_full" {
		t.Errorf("should stop canary container, got %s", docker.stopCalls[0])
	}

	s, err := stateMgr.Load("app")
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if s.Status != state.StatusIdle {
		t.Errorf("expected idle, got %s", s.Status)
	}
}

func TestCanaryLargeStep(t *testing.T) {
	docker := &mockDocker{}
	prov := &mockProvider{}
	stateMgr := state.NewManager(t.TempDir())

	c := NewCanary(docker, prov, stateMgr)
	d := canaryDeployment()
	d.Config.Canary.StartPercentage = 50
	d.Config.Canary.Step = 50

	if err := c.Execute(context.Background(), d); err != nil {
		t.Fatalf("execute: %v", err)
	}

	// 50% → exits loop at 100%, then final = 2 configs
	if len(prov.configs) != 2 {
		t.Errorf("expected 2 configs (1 canary step + final), got %d", len(prov.configs))
	}
}

func TestCanaryContextCancel(t *testing.T) {
	docker := &mockDocker{}
	prov := &mockProvider{}
	stateMgr := state.NewManager(t.TempDir())

	c := NewCanary(docker, prov, stateMgr)
	d := canaryDeployment()
	d.Config.Canary.Interval = 5 * time.Second

	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()

	err := c.Execute(ctx, d)
	if err == nil {
		t.Fatal("expected context error")
	}
}
