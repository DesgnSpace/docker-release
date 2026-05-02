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
		"release.affinity":                "cookie",
		"release.bg.soak_time":            "2m",
		"release.bg.green_weight":         "60",
		"release.canary.start_percentage": "25",
		"release.canary.step":             "10",
		"release.canary.interval":         "1m",
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
	if cfg.Affinity != "cookie" {
		t.Errorf("affinity = %s, want cookie", cfg.Affinity)
	}
	if cfg.BlueGreen.SoakTime != 2*time.Minute {
		t.Errorf("soak_time = %v, want 2m", cfg.BlueGreen.SoakTime)
	}
	if cfg.BlueGreen.GreenWeight != 60 {
		t.Errorf("green_weight = %d, want 60", cfg.BlueGreen.GreenWeight)
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
	if cfg.BlueGreen.GreenWeight != 50 {
		t.Errorf("default green_weight = %d, want 50", cfg.BlueGreen.GreenWeight)
	}
	if cfg.Affinity != "cookie" {
		t.Errorf("default affinity = %s, want cookie", cfg.Affinity)
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
		"release.provider": "unknown-lb",
	}

	_, err := ParseLabels(labels)
	if err == nil {
		t.Fatal("expected error for unknown provider")
	}
}

func TestParseLabelsCaddy(t *testing.T) {
	labels := map[string]string{
		"release.enable":            "true",
		"release.provider":          "caddy",
		"release.strategy":          "linear",
		"release.caddy.container":   "caddy",
		"release.caddy.config_dir":  "/etc/caddy/conf.d",
		"release.caddy.keepalive":   "5",
	}

	cfg, err := ParseLabels(labels)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Provider != ProviderCaddy {
		t.Errorf("provider = %s, want caddy", cfg.Provider)
	}
	if cfg.CaddyContainer != "caddy" {
		t.Errorf("caddy_container = %s, want caddy", cfg.CaddyContainer)
	}
	if cfg.CaddyConfigDir != "/etc/caddy/conf.d" {
		t.Errorf("caddy_config_dir = %s, want /etc/caddy/conf.d", cfg.CaddyConfigDir)
	}
	if cfg.CaddyKeepalive != 5 {
		t.Errorf("caddy_keepalive = %d, want 5", cfg.CaddyKeepalive)
	}
}

func TestParseLabelsHAProxy(t *testing.T) {
	labels := map[string]string{
		"release.enable":              "true",
		"release.provider":            "haproxy",
		"release.strategy":            "linear",
		"release.haproxy.container":   "haproxy",
		"release.haproxy.config_dir":  "/etc/haproxy/conf.d",
	}

	cfg, err := ParseLabels(labels)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Provider != ProviderHAProxy {
		t.Errorf("provider = %s, want haproxy", cfg.Provider)
	}
	if cfg.HAProxyContainer != "haproxy" {
		t.Errorf("haproxy_container = %s, want haproxy", cfg.HAProxyContainer)
	}
	if cfg.HAProxyConfigDir != "/etc/haproxy/conf.d" {
		t.Errorf("haproxy_config_dir = %s, want /etc/haproxy/conf.d", cfg.HAProxyConfigDir)
	}
}

func TestParseLabelsNoneCanaryRejected(t *testing.T) {
	labels := map[string]string{
		"release.enable":   "true",
		"release.provider": "none",
		"release.strategy": "canary",
	}

	_, err := ParseLabels(labels)
	if err == nil {
		t.Fatal("expected error: provider=none with strategy=canary should be rejected")
	}
}

func TestParseLabelsNoneBlueGreenRejected(t *testing.T) {
	labels := map[string]string{
		"release.enable":   "true",
		"release.provider": "none",
		"release.strategy": "blue-green",
	}

	_, err := ParseLabels(labels)
	if err == nil {
		t.Fatal("expected error: provider=none with strategy=blue-green should be rejected")
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

func TestParseLabelsInvalidBlueGreenWeight(t *testing.T) {
	labels := map[string]string{
		"release.enable":          "true",
		"release.bg.green_weight": "0",
	}

	_, err := ParseLabels(labels)
	if err == nil {
		t.Fatal("expected error for blue-green weight < 1")
	}
}

func TestParseLabelsInvalidAffinity(t *testing.T) {
	labels := map[string]string{
		"release.enable":   "true",
		"release.affinity": "random",
	}

	_, err := ParseLabels(labels)
	if err == nil {
		t.Fatal("expected error for invalid affinity")
	}
}

func TestParseLabelsDisabledAffinity(t *testing.T) {
	labels := map[string]string{
		"release.enable":   "true",
		"release.affinity": "",
	}

	cfg, err := ParseLabels(labels)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Affinity != "" {
		t.Errorf("affinity = %q, want empty", cfg.Affinity)
	}
}
