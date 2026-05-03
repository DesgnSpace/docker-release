# Traefik Provider

Use this when your app runs behind Traefik.

`docker-release` writes dynamic YAML to a shared volume. Traefik watches that folder and reloads it.

## When to Use This

Use this provider when Traefik already handles your routes.

Traefik router labels stay on your app service. `docker-release` only writes the file-provider service that contains the live backend list.

## What Gets Written

For an app named `app`, `docker-release` writes a file like this:

```yaml
http:
  services:
    app:
      loadBalancer:
        servers:
          - url: "http://172.18.0.4:80"
          - url: "http://172.18.0.5:80"
```

Your router must point to `app@file`.

## Compose Example

```yaml
services:
  docker-release:
    image: malico/docker-release:0.1
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
      - traefik-config:/shared/traefik-config:rw # docker-release writes Traefik YAML here

  traefik:
    image: traefik:v3
    ports:
      - "80:80"
      - "8080:8080"
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
      - traefik-config:/etc/traefik/dynamic:ro # Traefik reads generated YAML here
    depends_on:
      docker-release:
        condition: service_healthy # wait until docker-release has written service files
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
  traefik-config:
```

## Required Labels

```yaml
release.enable: "true"
release.provider: traefik
traefik.http.routers.app.service: "app@file"
```

## Optional Overrides

| Label | Default | Override when |
|---|---|---|
| `release.traefik.config_dir` | `/shared/traefik-config` | shared volume mounted at a different path |

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
# No label needed. Linear is the default.
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
