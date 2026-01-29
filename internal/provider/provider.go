package provider

type Server struct {
	Addr   string // e.g. "172.18.0.5:80"
	Weight int    // 0 means no weight directive
	Down   bool   // marks server as draining
}

type UpstreamState struct {
	Service   string
	Servers   []Server
	Affinity  string // "ip", "cookie", or ""
}

type Provider interface {
	GenerateConfig(state *UpstreamState) error
	Reload() error
}
