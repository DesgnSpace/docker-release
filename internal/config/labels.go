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
	ProviderTraefik    ProviderType = "traefik"
	ProviderNone       ProviderType = "none"
)

type HealthCheckConfig struct {
	Path        string
	Interval    time.Duration
	Timeout     time.Duration
	Retries     int
	StartPeriod time.Duration
}

type ServiceConfig struct {
	Enabled            bool
	Provider           ProviderType
	Strategy           Strategy
	HealthCheckTimeout time.Duration
	DrainTimeout       time.Duration
	NginxContainer     string
	NginxConfigDir     string
	TraefikConfigDir   string
	UpstreamName       string

	BlueGreen   BlueGreenConfig
	Canary      CanaryConfig
	HealthCheck HealthCheckConfig
}

type BlueGreenConfig struct {
	SoakTime time.Duration
}

type CanaryConfig struct {
	StartPercentage int
	Step            int
	Interval        time.Duration
	Affinity        string
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
		NginxContainer:     getOr(labels, "release.nginx.container", ""),
		NginxConfigDir:     getOr(labels, "release.nginx.config_dir", ""),
		TraefikConfigDir:   getOr(labels, "release.traefik.config_dir", ""),
		UpstreamName:       getOr(labels, "release.upstream", ""),

		BlueGreen: BlueGreenConfig{
			SoakTime: parseDurationOr(labels, "release.bg.soak_time", 5*time.Minute),
		},

		Canary: CanaryConfig{
			StartPercentage: parseIntOr(labels, "release.canary.start_percentage", 10),
			Step:            parseIntOr(labels, "release.canary.step", 20),
			Interval:        parseDurationOr(labels, "release.canary.interval", 2*time.Minute),
			Affinity:        getOr(labels, "release.canary.affinity", "ip"),
		},

		HealthCheck: HealthCheckConfig{
			Path:        getOr(labels, "release.healthcheck.path", ""),
			Interval:    parseDurationOr(labels, "release.healthcheck.interval", 5*time.Second),
			Timeout:     parseDurationOr(labels, "release.healthcheck.timeout", 5*time.Second),
			Retries:     parseIntOr(labels, "release.healthcheck.retries", 3),
			StartPeriod: parseDurationOr(labels, "release.healthcheck.start_period", 0),
		},
	}

	if err := cfg.validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

func (c *ServiceConfig) validate() error {
	switch c.Provider {
	case ProviderNginxProxy, ProviderNginx, ProviderTraefik, ProviderNone:
	default:
		return fmt.Errorf("unknown provider: %s", c.Provider)
	}

	switch c.Strategy {
	case StrategyLinear, StrategyBlueGreen, StrategyCanary:
	default:
		return fmt.Errorf("unknown strategy: %s", c.Strategy)
	}

	if c.Canary.StartPercentage < 1 || c.Canary.StartPercentage > 100 {
		return fmt.Errorf("canary.start_percentage must be 1-100, got %d", c.Canary.StartPercentage)
	}

	if c.Canary.Step < 1 || c.Canary.Step > 100 {
		return fmt.Errorf("canary.step must be 1-100, got %d", c.Canary.Step)
	}

	return nil
}

func getOr(labels map[string]string, key, fallback string) string {
	if v, ok := labels[key]; ok && v != "" {
		return v
	}
	return fallback
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
