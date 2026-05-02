# Traefik Provider

Use this when your app runs behind Traefik.

`docker-release` writes dynamic YAML to a shared volume. Traefik watches that folder and reloads it.

## Compose Example

```yaml
services:
  docker-release:
    image: malico/docker-release:0.1
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
      - docker-release-state:/var/lib/docker-release
      - traefik-config:/shared/traefik-config:rw

  traefik:
    image: traefik:v3
    ports:
      - "80:80"
      - "8080:8080"
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
      - traefik-config:/etc/traefik/dynamic:ro
    command:
      - --api.insecure=true
      - --providers.docker=true
      - --providers.docker.exposedbydefault=false
      - --providers.file.directory=/etc/traefik/dynamic
      - --providers.file.watch=true

  app:
    image: your-registry/app:latest
    labels:
      release.enable: "true"
      release.provider: traefik
      release.strategy: linear
      release.traefik.config_dir: /shared/traefik-config
      traefik.enable: "true"
      traefik.http.routers.app.rule: "PathPrefix(`/app`)"
      traefik.http.routers.app.service: "app@file"
      traefik.http.routers.app.middlewares: "strip-app"
      traefik.http.middlewares.strip-app.stripprefix.prefixes: "/app"
    healthcheck:
      test: ["CMD", "wget", "-qO-", "http://localhost/health"]
      interval: 10s
      timeout: 5s
      retries: 3

volumes:
  docker-release-state:
  traefik-config:
```

## Required Labels

```yaml
release.enable: "true"
release.provider: traefik
release.traefik.config_dir: /shared/traefik-config
traefik.http.routers.app.service: "app@file"
```

## Deploy

```sh
docker compose up -d
docker release app
```

## Notes

- Use `app@file`. This points Traefik to the service file that `docker-release` writes.
- `release.affinity: ip` uses Traefik HRW routing.
- `release.affinity: cookie` uses a generated cookie name like `_srr_a172cedcae`.

## Strategy Examples

### Linear

```yaml
release.strategy: linear
release.drain_timeout: 10s
release.health_check_timeout: 60s
```

### Canary

```yaml
release.strategy: canary
release.canary.start_percentage: 10
release.canary.step: 20
release.canary.interval: 2m
release.affinity: cookie
```

### Blue/Green

```yaml
release.strategy: blue-green
release.bg.soak_time: 5m
release.bg.green_weight: 50
release.affinity: ip
```

## Path Routing Example

```yaml
traefik.enable: "true"
traefik.http.routers.app.rule: "PathPrefix(`/app`)"
traefik.http.routers.app.service: "app@file"
traefik.http.routers.app.middlewares: "strip-app"
traefik.http.middlewares.strip-app.stripprefix.prefixes: "/app"
```

## Host Routing Example

```yaml
traefik.enable: "true"
traefik.http.routers.app.rule: "Host(`app.example.com`)"
traefik.http.routers.app.service: "app@file"
```

## File Provider Settings

```yaml
command:
  - --providers.file.directory=/etc/traefik/dynamic
  - --providers.file.watch=true
```

## Common Problems

| Problem | Fix |
|---|---|
| Traefik routes to Docker service directly | Set service to `app@file`. |
| Dynamic config does not load | Check the shared volume mount to `/etc/traefik/dynamic`. |
| IP affinity uses cookies | Use `release.affinity: ip`; Traefik will use `hrw`. |
