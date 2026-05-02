package provider

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenderTraefikBasic(t *testing.T) {
	state := &UpstreamState{
		Service: "app",
		Servers: []Server{
			{Addr: "172.18.0.5:80"},
			{Addr: "172.18.0.6:80"},
		},
	}

	got := renderTraefikYAML(state)

	expects := []string{
		"app:",
		"loadBalancer:",
		`url: "http://172.18.0.5:80"`,
		`url: "http://172.18.0.6:80"`,
		"rule: \"Host(`app.local`)\"",
		"service: app",
	}

	for _, want := range expects {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}

	if strings.Contains(got, "weighted") {
		t.Error("should not have weighted section without weights")
	}

	if strings.Contains(got, "sticky") {
		t.Error("should not have sticky without affinity")
	}
}

func TestRenderTraefikWithCookieAffinity(t *testing.T) {
	state := &UpstreamState{
		Service:  "app",
		Affinity: "cookie",
		Servers: []Server{
			{Addr: "172.18.0.5:80"},
		},
	}

	got := renderTraefikYAML(state)

	expects := []string{
		"sticky:",
		"cookie:",
		"name: app_affinity",
	}

	for _, want := range expects {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

func TestRenderTraefikWeighted(t *testing.T) {
	state := &UpstreamState{
		Service:  "app",
		Affinity: "cookie",
		Servers: []Server{
			{Addr: "172.18.0.5:80", Weight: 90},
			{Addr: "172.18.0.6:80", Weight: 90},
			{Addr: "172.18.0.8:80", Weight: 10},
		},
	}

	got := renderTraefikYAML(state)

	expects := []string{
		"weighted:",
		"app-stable:",
		"app-canary:",
		"weight: 90",
		"weight: 10",
		`url: "http://172.18.0.5:80"`,
		`url: "http://172.18.0.6:80"`,
		`url: "http://172.18.0.8:80"`,
		"sticky:",
		"name: app_affinity",
	}

	for _, want := range expects {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

func TestRenderTraefikWeightedBlueGreenCutover(t *testing.T) {
	state := &UpstreamState{
		Service:  "blue_green_app",
		Affinity: "ip",
		Servers: []Server{
			{Addr: "172.18.0.5:80", Weight: 0},
			{Addr: "172.18.0.6:80", Weight: 0},
			{Addr: "172.18.0.8:80", Weight: 100},
			{Addr: "172.18.0.9:80", Weight: 100},
		},
	}

	got := renderTraefikYAML(state)

	expects := []string{
		"weighted:",
		"blue_green_app-stable:",
		"blue_green_app-canary:",
		"weight: 100",
		`url: "http://172.18.0.5:80"`,
		`url: "http://172.18.0.6:80"`,
		`url: "http://172.18.0.8:80"`,
		`url: "http://172.18.0.9:80"`,
	}

	for _, want := range expects {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}

	if strings.Contains(got, "weight: 0") {
		t.Errorf("zero-weight service should be omitted from weighted route:\n%s", got)
	}
}

func TestRenderTraefikSkipsDownServers(t *testing.T) {
	state := &UpstreamState{
		Service: "app",
		Servers: []Server{
			{Addr: "172.18.0.5:80"},
			{Addr: "172.18.0.6:80", Down: true},
		},
	}

	got := renderTraefikYAML(state)

	if !strings.Contains(got, `url: "http://172.18.0.5:80"`) {
		t.Error("missing active server")
	}
	if strings.Contains(got, "172.18.0.6") {
		t.Error("down server should be excluded")
	}
}

func TestTraefikGenerateConfigWritesFile(t *testing.T) {
	dir := t.TempDir()
	p := NewTraefik(dir)

	state := &UpstreamState{
		Service: "webapp",
		Servers: []Server{
			{Addr: "172.18.0.5:3000"},
		},
	}

	if err := p.GenerateConfig(state); err != nil {
		t.Fatalf("generate error: %v", err)
	}

	path := filepath.Join(dir, "webapp.yml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read error: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "webapp:") {
		t.Error("file missing service name")
	}
	if !strings.Contains(content, `url: "http://172.18.0.5:3000"`) {
		t.Error("file missing server url")
	}

	tmp := path + ".tmp"
	if _, err := os.Stat(tmp); !os.IsNotExist(err) {
		t.Error("temp file not cleaned up")
	}
}

func TestTraefikReloadIsNoop(t *testing.T) {
	p := NewTraefik("/tmp/test")

	if err := p.Reload(); err != nil {
		t.Errorf("reload should be no-op, got: %v", err)
	}
}
