package config

import (
	"testing"
	"time"
)

func TestParseLabels(t *testing.T) {
	labels := map[string]string{
		"release.enable":                  "true",
		"release.provider":                "nginx-proxy",
		"release.strategy":                "canary",
		"release.health_check_timeout":    "30s",
		"release.bg.soak_time":            "2m",
		"release.canary.start_percentage": "25",
		"release.canary.step":             "10",
		"release.canary.interval":         "1m",
		"release.canary.affinity":         "cookie",
	}

	cfg, err := ParseLabels(labels)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Provider != ProviderNginxProxy {
		t.Errorf("provider = %s, want nginx-proxy", cfg.Provider)
	}
	if cfg.Strategy != StrategyCanary {
		t.Errorf("strategy = %s, want canary", cfg.Strategy)
	}
	if cfg.HealthCheckTimeout != 30*time.Second {
		t.Errorf("health_check_timeout = %v, want 30s", cfg.HealthCheckTimeout)
	}
	if cfg.BlueGreen.SoakTime != 2*time.Minute {
		t.Errorf("soak_time = %v, want 2m", cfg.BlueGreen.SoakTime)
	}
	if cfg.Canary.StartPercentage != 25 {
		t.Errorf("start_percentage = %d, want 25", cfg.Canary.StartPercentage)
	}
	if cfg.Canary.Step != 10 {
		t.Errorf("step = %d, want 10", cfg.Canary.Step)
	}
	if cfg.Canary.Interval != 1*time.Minute {
		t.Errorf("interval = %v, want 1m", cfg.Canary.Interval)
	}
	if cfg.Canary.Affinity != "cookie" {
		t.Errorf("affinity = %s, want cookie", cfg.Canary.Affinity)
	}
}

func TestParseLabelsDefaults(t *testing.T) {
	labels := map[string]string{
		"release.enable": "true",
	}

	cfg, err := ParseLabels(labels)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Provider != ProviderNginxProxy {
		t.Errorf("default provider = %s, want nginx-proxy", cfg.Provider)
	}
	if cfg.Strategy != StrategyLinear {
		t.Errorf("default strategy = %s, want linear", cfg.Strategy)
	}
	if cfg.HealthCheckTimeout != 60*time.Second {
		t.Errorf("default timeout = %v, want 60s", cfg.HealthCheckTimeout)
	}
	if cfg.Canary.StartPercentage != 10 {
		t.Errorf("default start_percentage = %d, want 10", cfg.Canary.StartPercentage)
	}
	if cfg.Canary.Step != 20 {
		t.Errorf("default step = %d, want 20", cfg.Canary.Step)
	}
}

func TestParseLabelsNotEnabled(t *testing.T) {
	labels := map[string]string{
		"release.enable": "false",
	}

	_, err := ParseLabels(labels)
	if err == nil {
		t.Fatal("expected error for disabled release")
	}
}

func TestParseLabelsInvalidProvider(t *testing.T) {
	labels := map[string]string{
		"release.enable":   "true",
		"release.provider": "haproxy",
	}

	_, err := ParseLabels(labels)
	if err == nil {
		t.Fatal("expected error for unknown provider")
	}
}

func TestParseLabelsInvalidStrategy(t *testing.T) {
	labels := map[string]string{
		"release.enable":   "true",
		"release.strategy": "yolo",
	}

	_, err := ParseLabels(labels)
	if err == nil {
		t.Fatal("expected error for unknown strategy")
	}
}

func TestParseLabelsInvalidPercentage(t *testing.T) {
	labels := map[string]string{
		"release.enable":                  "true",
		"release.canary.start_percentage": "0",
	}

	_, err := ParseLabels(labels)
	if err == nil {
		t.Fatal("expected error for percentage < 1")
	}
}
