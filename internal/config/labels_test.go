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
		"release.nginx.container":         "my-nginx",
		"release.nginx.keepalive":         "20",
	}

	cfg, err := ParseLabels(labels)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.NginxContainer != "my-nginx" {
		t.Errorf("nginx_container = %s, want my-nginx", cfg.NginxContainer)
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
	if cfg.NginxKeepalive != 20 {
		t.Errorf("nginx.keepalive = %d, want 20", cfg.NginxKeepalive)
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
	if cfg.NginxContainer != "" {
		t.Errorf("default nginx_container = %s, want empty", cfg.NginxContainer)
	}
	if cfg.NginxKeepalive != -1 {
		t.Errorf("default nginx_keepalive = %d, want -1", cfg.NginxKeepalive)
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

func TestParseLabelsNoneProvider(t *testing.T) {
	labels := map[string]string{
		"release.enable":   "true",
		"release.provider": "none",
		"release.strategy": "linear",
	}

	cfg, err := ParseLabels(labels)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Provider != ProviderNone {
		t.Errorf("provider = %s, want none", cfg.Provider)
	}
}

func TestParseLabelsHealthCheck(t *testing.T) {
	labels := map[string]string{
		"release.enable":                   "true",
		"release.healthcheck.path":         "/health",
		"release.healthcheck.interval":     "10s",
		"release.healthcheck.timeout":      "3s",
		"release.healthcheck.retries":      "5",
		"release.healthcheck.start_period": "30s",
	}

	cfg, err := ParseLabels(labels)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.HealthCheck.Path != "/health" {
		t.Errorf("healthcheck.path = %s, want /health", cfg.HealthCheck.Path)
	}
	if cfg.HealthCheck.Interval != 10*time.Second {
		t.Errorf("healthcheck.interval = %v, want 10s", cfg.HealthCheck.Interval)
	}
	if cfg.HealthCheck.Timeout != 3*time.Second {
		t.Errorf("healthcheck.timeout = %v, want 3s", cfg.HealthCheck.Timeout)
	}
	if cfg.HealthCheck.Retries != 5 {
		t.Errorf("healthcheck.retries = %d, want 5", cfg.HealthCheck.Retries)
	}
	if cfg.HealthCheck.StartPeriod != 30*time.Second {
		t.Errorf("healthcheck.start_period = %v, want 30s", cfg.HealthCheck.StartPeriod)
	}
}

func TestParseLabelsHealthCheckDefaults(t *testing.T) {
	labels := map[string]string{
		"release.enable": "true",
	}

	cfg, err := ParseLabels(labels)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.HealthCheck.Path != "" {
		t.Errorf("default healthcheck.path = %s, want empty", cfg.HealthCheck.Path)
	}
	if cfg.HealthCheck.Interval != 5*time.Second {
		t.Errorf("default healthcheck.interval = %v, want 5s", cfg.HealthCheck.Interval)
	}
	if cfg.HealthCheck.Timeout != 5*time.Second {
		t.Errorf("default healthcheck.timeout = %v, want 5s", cfg.HealthCheck.Timeout)
	}
	if cfg.HealthCheck.Retries != 3 {
		t.Errorf("default healthcheck.retries = %d, want 3", cfg.HealthCheck.Retries)
	}
	if cfg.HealthCheck.StartPeriod != 0 {
		t.Errorf("default healthcheck.start_period = %v, want 0", cfg.HealthCheck.StartPeriod)
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
