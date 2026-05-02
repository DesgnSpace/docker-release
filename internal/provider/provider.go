package provider

import (
	"fmt"
	"regexp"
	"strings"
)

var validName = regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)

func (u *UpstreamState) Validate() error {
	if !validName.MatchString(u.Service) {
		return fmt.Errorf("service name %q contains invalid characters", u.Service)
	}
	if name := u.ResolveUpstreamName(); !validName.MatchString(name) {
		return fmt.Errorf("upstream name %q contains invalid characters", name)
	}
	return nil
}

// matchesImage checks if image (e.g. "nginx:alpine") contains any of the expected keywords.
// Used in Reload to guard against reloading the wrong container.
func matchesImage(image string, keywords ...string) bool {
	img := strings.ToLower(image)
	for _, kw := range keywords {
		if strings.Contains(img, strings.ToLower(kw)) {
			return true
		}
	}
	return false
}

type Server struct {
	Addr   string // e.g. "172.18.0.5:80"
	Weight int    // 0 means no weight directive
	Down   bool   // marks server as draining
	Group  string // optional traffic group for weighted providers
}

type UpstreamState struct {
	Service      string
	UpstreamName string // overrides Service for upstream naming (e.g. VIRTUAL_HOST for nginx-proxy)
	Servers      []Server
	Affinity     string // "cookie" (default), "ip", or "" (disabled)
	             // cookie: nginx→ip_hash (OSS has no sticky), angie→sticky cookie, traefik→sticky.cookie
	             // ip: nginx/angie→ip_hash, traefik→sticky.cookie (no ip-hash in traefik)
	Keepalive    int    // 0 disables keepalive
}

func (u *UpstreamState) ResolveUpstreamName() string {
	if u.UpstreamName != "" {
		return u.UpstreamName
	}
	return u.Service
}

type Provider interface {
	GenerateConfig(state *UpstreamState) error
	Reload() error
}
