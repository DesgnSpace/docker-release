package config

import (
	"fmt"
	"strconv"
	"time"
)

type Strategy string

const (
	StrategyLinear    Strategy = "linear"
	StrategyBlueGreen Strategy = "blue-green"
	StrategyCanary    Strategy = "canary"
)

type ProviderType string

const (
	ProviderNginxProxy ProviderType = "nginx-proxy"
	ProviderNginx      ProviderType = "nginx"
	ProviderAngie      ProviderType = "angie"
	ProviderTraefik    ProviderType = "traefik"
	ProviderCaddy      ProviderType = "caddy"
	ProviderHAProxy    ProviderType = "haproxy"
	ProviderNone       ProviderType = "none"
)

type ServiceConfig struct {
	Enabled            bool
	Provider           ProviderType
	Strategy           Strategy
	HealthCheckTimeout time.Duration
	DrainTimeout       time.Duration
	Affinity           string
	NginxContainer     string
	NginxConfigDir     string
	NginxKeepalive     int
	AngieContainer     string
	AngieConfigDir     string
	AngieKeepalive     int
	TraefikConfigDir   string
	CaddyContainer     string
	CaddyConfigDir     string
	CaddyPath          string
	CaddyKeepalive     int
	HAProxyContainer   string
	HAProxyConfigDir   string
	UpstreamName       string

	BlueGreen BlueGreenConfig
	Canary    CanaryConfig
}

type BlueGreenConfig struct {
	SoakTime    time.Duration
	GreenWeight int
}

type CanaryConfig struct {
	StartPercentage int
	Step            int
	Interval        time.Duration
}

func ParseLabels(labels map[string]string) (*ServiceConfig, error) {
	if labels["release.enable"] != "true" {
		return nil, fmt.Errorf("release.enable is not true")
	}

	cfg := &ServiceConfig{
		Enabled:            true,
		Provider:           ProviderType(getOr(labels, "release.provider", "nginx-proxy")),
		Strategy:           Strategy(getOr(labels, "release.strategy", "linear")),
		HealthCheckTimeout: parseDurationOr(labels, "release.health_check_timeout", 60*time.Second),
		DrainTimeout:       parseDurationOr(labels, "release.drain_timeout", 10*time.Second),
		Affinity:           resolveAffinity(labels),
		NginxContainer:     getOr(labels, "release.nginx.container", ""),
		NginxConfigDir:     getOr(labels, "release.nginx.config_dir", ""),
		NginxKeepalive:     parseIntOr(labels, "release.nginx.keepalive", -1),
		AngieContainer:     getOr(labels, "release.angie.container", ""),
		AngieConfigDir:     getOr(labels, "release.angie.config_dir", ""),
		AngieKeepalive:     parseIntOr(labels, "release.angie.keepalive", -1),
		TraefikConfigDir:   getOr(labels, "release.traefik.config_dir", ""),
		CaddyContainer:     getOr(labels, "release.caddy.container", ""),
		CaddyConfigDir:     getOr(labels, "release.caddy.config_dir", ""),
		CaddyPath:          getOr(labels, "release.caddy.path", ""),
		CaddyKeepalive:     parseIntOr(labels, "release.caddy.keepalive", -1),
		HAProxyContainer:   getOr(labels, "release.haproxy.container", ""),
		HAProxyConfigDir:   getOr(labels, "release.haproxy.config_dir", ""),
		UpstreamName:       getOr(labels, "release.upstream", ""),

		BlueGreen: BlueGreenConfig{
			SoakTime:    parseDurationOr(labels, "release.bg.soak_time", 5*time.Minute),
			GreenWeight: parseIntOr(labels, "release.bg.green_weight", 50),
		},

		Canary: CanaryConfig{
			StartPercentage: parseIntOr(labels, "release.canary.start_percentage", 10),
			Step:            parseIntOr(labels, "release.canary.step", 20),
			Interval:        parseDurationOr(labels, "release.canary.interval", 2*time.Minute),
		},
	}

	if err := cfg.validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

func (c *ServiceConfig) validate() error {
	switch c.Provider {
	case ProviderNginxProxy, ProviderNginx, ProviderAngie, ProviderTraefik, ProviderCaddy, ProviderHAProxy, ProviderNone:
	default:
		return fmt.Errorf("unknown provider: %s", c.Provider)
	}

	if c.Provider == ProviderNone && (c.Strategy == StrategyCanary || c.Strategy == StrategyBlueGreen) {
		return fmt.Errorf("provider=none requires strategy=linear (canary and blue-green need a load balancer)")
	}

	switch c.Strategy {
	case StrategyLinear, StrategyBlueGreen, StrategyCanary:
	default:
		return fmt.Errorf("unknown strategy: %s", c.Strategy)
	}

	switch c.Affinity {
	case "cookie", "ip", "":
	default:
		return fmt.Errorf("affinity must be cookie, ip, or empty, got %q", c.Affinity)
	}

	if c.Canary.StartPercentage < 1 || c.Canary.StartPercentage > 100 {
		return fmt.Errorf("canary.start_percentage must be 1-100, got %d", c.Canary.StartPercentage)
	}

	if c.Canary.Step < 1 || c.Canary.Step > 100 {
		return fmt.Errorf("canary.step must be 1-100, got %d", c.Canary.Step)
	}

	if c.BlueGreen.GreenWeight < 1 || c.BlueGreen.GreenWeight > 100 {
		return fmt.Errorf("bg.green_weight must be 1-100, got %d", c.BlueGreen.GreenWeight)
	}

	if c.NginxKeepalive < -1 {
		return fmt.Errorf("nginx.keepalive must be >= 0, got %d", c.NginxKeepalive)
	}

	if c.AngieKeepalive < -1 {
		return fmt.Errorf("angie.keepalive must be >= 0, got %d", c.AngieKeepalive)
	}

	if c.CaddyKeepalive < -1 {
		return fmt.Errorf("caddy.keepalive must be >= 0, got %d", c.CaddyKeepalive)
	}

	return nil
}

func (c *ServiceConfig) ResolveNginxKeepalive(serverCount int) int {
	if c.NginxKeepalive >= 0 {
		return c.NginxKeepalive
	}

	if serverCount <= 0 {
		return 0
	}

	return serverCount + 1
}

func (c *ServiceConfig) ResolveCaddyKeepalive(serverCount int) int {
	if c.CaddyKeepalive >= 0 {
		return c.CaddyKeepalive
	}

	if serverCount <= 0 {
		return 0
	}

	return serverCount + 1
}

func (c *ServiceConfig) ResolveAngieKeepalive(serverCount int) int {
	if c.AngieKeepalive >= 0 {
		return c.AngieKeepalive
	}

	if serverCount <= 0 {
		return 0
	}

	return serverCount + 1
}

func getOr(labels map[string]string, key, fallback string) string {
	if v, ok := labels[key]; ok && v != "" {
		return v
	}
	return fallback
}

func resolveAffinity(labels map[string]string) string {
	v, ok := labels["release.affinity"]
	if !ok {
		return "cookie"
	}
	return v
}

func parseIntOr(labels map[string]string, key string, fallback int) int {
	v, ok := labels[key]
	if !ok || v == "" {
		return fallback
	}

	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}

	return n
}

func parseDurationOr(labels map[string]string, key string, fallback time.Duration) time.Duration {
	v, ok := labels[key]
	if !ok || v == "" {
		return fallback
	}

	d, err := time.ParseDuration(v)
	if err != nil {
		return fallback
	}

	return d
}
