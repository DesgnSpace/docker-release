# docker-release

Zero-downtime deployment controller for Docker Compose. Runs as a sidecar, watches your services, and orchestrates rolling updates, blue/green, and canary releases — without Kubernetes.

---

## How it works

`docker-release` sits in your Compose stack and talks to the Docker socket. When you trigger a deploy, it:

1. Starts new containers on the updated image
2. Waits for them to pass health checks
3. Shifts traffic via your reverse proxy config
4. Drains and removes old containers

It never proxies traffic itself — it writes config to a shared volume that your proxy (Nginx, Angie, Traefik) hot-reloads.

---

## Quick start

Add to your `docker-compose.yml`:

```yaml
services:
  docker-release:
    image: your-registry/docker-release:latest
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
      - proxy-config:/shared/proxy-config:rw
    restart: unless-stopped

  app:
    image: your-registry/app:latest
    labels:
      release.enable: "true"
      release.provider: nginx
      release.strategy: linear
      release.nginx.service: nginx
      release.nginx.config_dir: /shared/proxy-config
    healthcheck:
      test: ["CMD", "wget", "-qO-", "http://localhost/health"]
      interval: 10s
      timeout: 5s
      retries: 3
```

Install the CLI plugin once, then deploy:

```sh
curl -fsSL https://raw.githubusercontent.com/malico/docker-release/main/scripts/docker-release \
  | sudo tee ~/.docker/cli-plugins/docker-release >/dev/null \
  && sudo chmod +x ~/.docker/cli-plugins/docker-release

docker compose up -d
docker release app
```

---

## Daily workflow

```sh
docker release app           # deploy
docker release app --force   # deploy, override an in-progress one
docker release rollback app  # roll back to the previous deployment
docker release status        # show state of all managed services
docker release status app    # show state of one service
```

---

## CLI plugin install

One-line install from the repo — no clone needed:

```sh
curl -fsSL https://raw.githubusercontent.com/malico/docker-release/main/scripts/docker-release \
  | sudo tee ~/.docker/cli-plugins/docker-release >/dev/null \
  && sudo chmod +x ~/.docker/cli-plugins/docker-release
```

The plugin auto-detects the active Compose project from your current directory — it works across all your stacks from a single install.

For contributors working in this repo:

```sh
make dev   # symlinks scripts/docker-release → ~/.docker/cli-plugins/docker-release
```

---

## Deployment strategies

### Linear (default)

Replaces containers one at a time. Each old container is drained and removed only after the replacement is healthy.

```yaml
release.strategy: linear
release.drain_timeout: 10s
release.health_check_timeout: 60s
```

### Blue/Green

Spins up a full replacement set, waits for all to be healthy, then cuts over all traffic at once. Holds the old set during a soak period for instant rollback.

```yaml
release.strategy: blue-green
release.bg.soak_time: 5m
release.bg.green_weight: 50
release.bg.affinity: ip
```

### Canary

Routes a configurable percentage of traffic to the new version and gradually increases it. Rolls back automatically if the canary becomes unhealthy.

```yaml
release.strategy: canary
release.canary.start_percentage: 10
release.canary.step: 20
release.canary.interval: 2m
release.canary.affinity: ip    # ip | cookie
```

---

## Providers

| Provider | Status | Notes |
|---|---|---|
| `nginx` | stable | Writes upstream conf to shared volume; reloads Nginx. [Details](./providers/nginx.md) |
| `angie` | stable | Same mechanism as Nginx for the Angie fork. |
| `traefik` | draft | Generates dynamic YAML via Traefik file provider. |
| `nginx-proxy` | draft | Writes `nginx.tmpl` for jwilder/nginx-proxy. |
| `none` | stable | Orchestration only — no proxy config written. |

All providers follow the same pattern: mount a shared config volume to both `docker-release` and your proxy, label your app services, and pick a strategy.

---

## Label reference

### Required

| Label | Description |
|---|---|
| `release.enable` | Set `true` to manage this service |
| `release.provider` | `nginx` \| `angie` \| `traefik` \| `nginx-proxy` \| `none` |

### Deployment

| Label | Default | Description |
|---|---|---|
| `release.strategy` | `linear` | `linear` \| `blue-green` \| `canary` |
| `release.health_check_timeout` | `60s` | Max wait for container to become healthy |
| `release.drain_timeout` | `10s` | Wait after removing from upstream before stopping |
| `release.upstream` | _(service name)_ | Override the upstream block name |

### Health checks

Use Docker-native `healthcheck:` on app services. `docker-release` waits for Docker's `healthy` status and listens for Docker health events from the socket.

### Nginx / Angie

| Label | Description |
|---|---|
| `release.nginx.service` | Nginx service name (for reload signal) |
| `release.nginx.config_dir` | Shared config volume path inside docker-release |
| `release.angie.service` | Angie service name |
| `release.angie.config_dir` | Shared config volume path inside docker-release |

### Blue/Green

| Label | Default | Description |
|---|---|---|
| `release.bg.soak_time` | `5m` | How long to hold old containers before removal |
| `release.bg.green_weight` | `50` | Traffic percentage to green during soak |
| `release.bg.affinity` | `ip` | Session affinity during soak: `ip` or `cookie` |

### Canary

| Label | Default | Description |
|---|---|---|
| `release.canary.start_percentage` | `10` | Initial traffic percentage to new version |
| `release.canary.step` | `20` | Percentage increase per interval |
| `release.canary.interval` | `2m` | Time between steps |
| `release.canary.affinity` | `ip` | Session affinity: `ip` or `cookie` |

---

## Full compose example

```yaml
name: myapp

services:
  docker-release:
    image: your-registry/docker-release:latest
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
      - proxy-config:/shared/proxy-config:rw
    restart: unless-stopped

  nginx:
    image: nginx:alpine
    ports:
      - "80:80"
    volumes:
      - proxy-config:/etc/nginx/conf.d/custom:ro
      - ./nginx.conf:/etc/nginx/conf.d/default.conf:ro

  app:
    image: your-registry/app:latest
    deploy:
      replicas: 2
    labels:
      release.enable: "true"
      release.provider: nginx
      release.strategy: linear
      release.nginx.service: nginx
      release.nginx.config_dir: /shared/proxy-config
    healthcheck:
      test: ["CMD", "wget", "-qO-", "http://localhost/health"]
      interval: 10s
      timeout: 5s
      retries: 3

volumes:
  proxy-config:
```

Base `nginx.conf`:

```nginx
server {
    listen 80;
    proxy_set_header Host $host;
    proxy_set_header X-Real-IP $remote_addr;

    location / {
        proxy_pass http://app_upstream/;
    }
}
```

---

## Rollback

`docker-release` persists deployment state to `/var/lib/docker-release`. Rolling back restores traffic to the previous container set and removes the new ones.

```sh
docker release rollback app
```

Rollback is also triggered automatically when a canary container fails health checks during a deployment.

---

## Notes

- Service names that match a reserved command word (`watch`, `status`, `rollback`, etc.) must use the explicit form: `docker release release <service>`.
- `watch` is the default container command — started by Compose, not manually.
- The Docker socket mount (`/var/run/docker.sock`) is required for the controller to manage containers.
