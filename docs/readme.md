# docker-release docs

This project watches your Docker-managed services and coordinates zero-downtime deployments via pluggable providers and strategies.

## Providers

- Nginx: production-style quickstart with compose, labels, and base config.  [view more](./providers/nginx.md)
- Traefik (draft): generates dynamic YAML; mount the rendered dir into Traefik (`release.provider=traefik`, `release.traefik.config_dir`).
- nginx-proxy (draft): uses `nginx.tmpl` for jwilder/nginx-proxy (`release.provider=nginx-proxy`, `release.nginx.config_dir`).
- Noop: `release.provider=none` when you only need deployment orchestration.

All providers follow the same pattern: mount a shared config directory to both `docker-release` and your proxy, set health check labels, and pick the rollout strategy that fits your needs.
