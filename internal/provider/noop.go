package provider

type NoopProvider struct{}

func NewNoop() *NoopProvider {
	return &NoopProvider{}
}

func (p *NoopProvider) GenerateConfig(_ *UpstreamState) error {
	return nil
}

func (p *NoopProvider) Reload() error {
	return nil
}
