package provider

type Server struct {
	Addr   string // e.g. "172.18.0.5:80"
	Weight int    // 0 means no weight directive
	Down   bool   // marks server as draining
}

type UpstreamState struct {
	Service      string
	UpstreamName string // overrides Service for upstream naming (e.g. VIRTUAL_HOST for nginx-proxy)
	Servers      []Server
	Affinity     string // "ip", "cookie", or ""
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
