# docker-release

Zero-downtime deployment controller for Docker Compose. Watches your services and coordinates rolling updates, blue/green, and canary releases — without Kubernetes.

## How it works

`docker-release` runs as a sidecar in your Compose stack. It listens to Docker events and, when it detects a new image on a managed service, orchestrates the cutover: spinning up new containers, shifting traffic through your reverse proxy, and draining old containers gracefully.

```
docker compose up -d   # new image pulled for "app"
docker release app     # deploy → zero-downtime rollout
```

---

## Requirements

- Docker Compose v2
- A reverse proxy in your stack (Nginx, Angie, Traefik, or nginx-proxy)

---

## Setup

### 1. Add docker-release to your compose file

```yaml
services:
  docker-release:
    image: your-registry/docker-release:latest
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
      - nginx-config:/shared/nginx-config:rw
    restart: unless-stopped
```

### 2. Label your app services

```yaml
  app:
    image: your-registry/app:latest
    labels:
      release.enable: "true"
      release.provider: nginx           # nginx | angie | traefik | nginx-proxy | none
      release.strategy: linear          # linear | blue-green | canary
      release.nginx.container: nginx
      release.nginx.config_dir: /shared/nginx-config
    healthcheck:
      test: ["CMD", "wget", "-qO-", "http://localhost/health"]
      interval: 10s
      timeout: 5s
      retries: 3
```

See [label reference](#label-reference) for all options.

### 3. Install the CLI plugin

One-line install — no repo clone needed:

```sh
curl -fsSL https://raw.githubusercontent.com/malico/docker-release/main/scripts/docker-release \
  | sudo tee ~/.docker/cli-plugins/docker-release >/dev/null \
  && sudo chmod +x ~/.docker/cli-plugins/docker-release
```

After install, from any project directory:

```sh
docker release app           # deploy
docker release status        # show state
docker release rollback app  # roll back
```

---

## Usage

```
docker release <service> [--force]   Deploy a service
docker release <command> [options]

  <service>                          Deploy the named service
  release <service> [--force]        Deploy explicitly; --force overrides in-progress
  rollback <service>                 Roll back to the previous deployment
  status [service]                   Show deployment state
  version                            Print version
```

Service names that match a reserved command word (e.g. a service literally named `status`) must use the explicit form: `docker release release status`.

---

## Full compose example (Nginx)

```yaml
name: myproject

services:
  docker-release:
    image: your-registry/docker-release:latest
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
      - nginx-config:/shared/nginx-config:rw
    restart: unless-stopped

  nginx:
    image: nginx:alpine
    ports:
      - "80:80"
    volumes:
      - nginx-config:/etc/nginx/conf.d/custom:ro
      - ./nginx.conf:/etc/nginx/conf.d/default.conf:ro

  app:
    image: your-registry/app:latest
    deploy:
      replicas: 2
    labels:
      release.enable: "true"
      release.provider: nginx
      release.strategy: linear
      release.nginx.container: nginx
      release.nginx.config_dir: /shared/nginx-config
    healthcheck:
      test: ["CMD", "wget", "-qO-", "http://localhost/health"]
      interval: 10s
      timeout: 5s
      retries: 3

volumes:
  nginx-config:
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

Routes a small percentage of traffic to the new version and gradually increases it. Rolls back automatically if the canary becomes unhealthy.

```yaml
release.strategy: canary
release.canary.start_percentage: 10
release.canary.step: 20
release.canary.interval: 2m
release.canary.affinity: ip    # ip | cookie
```

---

## Label reference

### App services

| Label | Default | Description |
|-------|---------|-------------|
| `release.enable` | — | Set `true` to manage this service |
| `release.provider` | `nginx-proxy` | `nginx` \| `angie` \| `traefik` \| `nginx-proxy` \| `none` |
| `release.strategy` | `linear` | `linear` \| `blue-green` \| `canary` |
| `release.health_check_timeout` | `60s` | Max wait for container to become healthy |
| `release.drain_timeout` | `10s` | Time to wait after removing from upstream before stopping |
| `release.upstream` | _(service name)_ | Override the upstream block name |
| `release.nginx.container` | — | Nginx service name (for reload) |
| `release.nginx.config_dir` | — | Shared config volume path inside docker-release |
| `release.nginx.keepalive` | _(auto)_ | Nginx keepalive connections per upstream |
| `release.angie.container` | — | Angie service name |
| `release.angie.config_dir` | — | Shared config volume path inside docker-release |
| `release.traefik.config_dir` | — | Shared config volume path inside docker-release |
| `release.bg.soak_time` | `5m` | Blue/Green: hold old containers this long before removal |
| `release.bg.green_weight` | `50` | Blue/Green: traffic percentage to green during soak |
| `release.bg.affinity` | `ip` | Blue/Green: session affinity during soak (`ip` or `cookie`) |
| `release.canary.start_percentage` | `10` | Canary: initial traffic percentage |
| `release.canary.step` | `20` | Canary: percentage increase per interval |
| `release.canary.interval` | `2m` | Canary: time between steps |
| `release.canary.affinity` | `ip` | Canary: session affinity (`ip` or `cookie`) |
Use Docker-native `healthcheck:` on app services. `docker-release` waits for Docker's `healthy` status and listens for Docker health events from the socket.

---

## Local development

Once the stack is running (`docker compose up -d`), run `make dev` to symlink the plugin script into your Docker CLI plugins directory. No Go toolchain needed — the binary lives inside Docker.

```sh
make dev   # symlinks scripts/docker-release → ~/.docker/cli-plugins/docker-release
```
