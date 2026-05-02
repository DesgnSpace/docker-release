package provider

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
