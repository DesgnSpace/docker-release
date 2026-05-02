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
- A reverse proxy in your stack (Nginx, Angie, Traefik, nginx-proxy, Caddy, or HAProxy) — or none for worker-only rollouts

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
      release.provider: nginx           # nginx | angie | traefik | nginx-proxy | caddy | haproxy | none
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

## Providers

### Nginx

Writes an upstream snippet to a shared volume and runs `nginx -s reload`.

> See a full working example in [`tests/nginx/`](tests/nginx/).

```yaml
services:
  docker-release:
    image: your-registry/docker-release:latest
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
      - nginx-config:/shared/nginx-config:rw

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

Your `nginx.conf` — the `include` pulls in the generated upstream snippets from the shared volume:

```nginx
# pull in generated upstream blocks from docker-release
include /etc/nginx/conf.d/custom/*.conf;

server {
    listen 80;

    location / {
        proxy_pass http://app_upstream/;
    }
}
```

---

### Angie

Drop-in replacement for Nginx. Uses `angie -s reload`. Config lands in `http.d/` (not `conf.d/`).

> See a full working example in [`tests/angie/`](tests/angie/).

```yaml
services:
  docker-release:
    image: your-registry/docker-release:latest
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
      - angie-config:/shared/angie-config:rw

  angie:
    image: docker.angie.software/angie:latest
    ports:
      - "80:80"
    volumes:
      - angie-config:/etc/angie/http.d/custom:ro
      - ./angie.conf:/etc/angie/http.d/default.conf:ro

  app:
    image: your-registry/app:latest
    deploy:
      replicas: 2
    labels:
      release.enable: "true"
      release.provider: angie
      release.strategy: linear
      release.angie.container: angie
      release.angie.config_dir: /shared/angie-config
    healthcheck:
      test: ["CMD", "wget", "-qO-", "http://localhost/health"]
      interval: 10s
      timeout: 5s
      retries: 3

volumes:
  angie-config:
```

Your `angie.conf` — note the include path is `http.d/` not `conf.d/` (Angie's layout differs from Nginx):

```nginx
# pull in generated upstream blocks from docker-release
include /etc/angie/http.d/custom/*.conf;

server {
    listen 80;

    location / {
        proxy_pass http://app_upstream/;
    }
}
```

---

### Caddy

Writes a `.caddy` snippet to a shared volume and runs `caddy reload`. When `release.caddy.path` is set, the generated snippet uses `handle_path` which automatically strips the prefix before proxying — no extra middleware needed.

> See a full working example in [`tests/caddy/`](tests/caddy/).

```yaml
services:
  docker-release:
    image: your-registry/docker-release:latest
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
      - caddy-config:/shared/caddy-config:rw

  caddy:
    image: caddy:alpine
    ports:
      - "80:80"
    volumes:
      - caddy-config:/etc/caddy/conf.d:ro
      - ./Caddyfile:/etc/caddy/Caddyfile:ro

  app:
    image: your-registry/app:latest
    deploy:
      replicas: 2
    labels:
      release.enable: "true"
      release.provider: caddy
      release.strategy: linear
      release.caddy.container: caddy
      release.caddy.config_dir: /shared/caddy-config
      release.caddy.path: /app
    healthcheck:
      test: ["CMD", "wget", "-qO-", "http://localhost/health"]
      interval: 10s
      timeout: 5s
      retries: 3

volumes:
  caddy-config:
```

**`Caddyfile`** — one `import` line is all you need. The glob is safe even before the first deploy; Caddy does not error on an empty match:

```caddy
:80 {
    import /etc/caddy/conf.d/*.caddy
}
```

**Extending the Caddyfile** — add any static routes or middleware alongside the import. The generated snippets slot in automatically:

```caddy
example.com {
    # static routes handled directly by Caddy
    handle /static* {
        root * /var/www
        file_server
    }

    # health endpoint served by Caddy itself
    respond /ping 200

    # managed services — docker-release writes these
    import /etc/caddy/conf.d/*.caddy
}
```

Each generated `.caddy` file looks like this (for `release.caddy.path: /app`):

```caddy
# Generated by docker-release for Caddy
handle_path /app* {
    reverse_proxy 172.18.0.4:80 172.18.0.5:80 {
        lb_policy round_robin
    }
}
```

The `handle_path` directive strips `/app` from the request before it reaches the upstream, so your containers see `/` not `/app`.

---

### HAProxy

Writes a backend `.cfg` snippet to a shared volume and sends `SIGUSR2` to PID 1 for a zero-downtime hot reload. Requires HAProxy running in master-worker mode (`-W` flag).

> See a full working example in [`tests/haproxy/`](tests/haproxy/).

```yaml
services:
  docker-release:
    image: your-registry/docker-release:latest
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
      - haproxy-config:/shared/haproxy-config:rw

  haproxy:
    image: haproxy:lts-alpine
    ports:
      - "80:80"
    volumes:
      - haproxy-config:/etc/haproxy/conf.d:ro
      - ./haproxy.cfg:/etc/haproxy/haproxy.cfg:ro
    command: ["haproxy", "-W", "-f", "/etc/haproxy/haproxy.cfg", "-f", "/etc/haproxy/conf.d"]

  app:
    image: your-registry/app:latest
    deploy:
      replicas: 2
    labels:
      release.enable: "true"
      release.provider: haproxy
      release.strategy: linear
      release.haproxy.container: haproxy
      release.haproxy.config_dir: /shared/haproxy-config
    healthcheck:
      test: ["CMD", "wget", "-qO-", "http://localhost/health"]
      interval: 10s
      timeout: 5s
      retries: 3

volumes:
  haproxy-config:
```

**`haproxy.cfg`** — your static config defines globals, defaults, and the frontend. The `stats socket` line with `expose-fd listeners` is required for socket inheritance across hot reloads. Backend blocks are generated by `docker-release` and loaded from `/etc/haproxy/conf.d/` on each reload.

```haproxy
global
    master-worker
    log stdout format raw local0
    stats socket /tmp/haproxy.sock mode 660 level admin expose-fd listeners

defaults
    mode http
    timeout connect 5s
    timeout client 30s
    timeout server 30s
    option http-keep-alive
    log global

frontend http
    bind *:80
    use_backend app_be if { path_beg /app }
```

**Extending `haproxy.cfg`** — add ACLs and `use_backend` lines for each managed service. Path rewriting (stripping the prefix) needs to happen before backend selection; use a `txn` variable to capture the routing decision first:

```haproxy
frontend http
    bind *:80

    acl is_app    path_beg /app
    acl is_api    path_beg /api

    # capture routing decision before rewriting path
    http-request set-var(txn.be) str(app) if is_app
    http-request set-var(txn.be) str(api) if is_api

    # strip prefix so upstream containers see /
    http-request set-path %[path,regsub(^/app,/)] if { var(txn.be) -m str app }
    http-request set-path %[path,regsub(^/api,/)] if { var(txn.be) -m str api }

    use_backend app_be if { var(txn.be) -m str app }
    use_backend api_be if { var(txn.be) -m str api }
```

Each generated `.cfg` file looks like this:

```haproxy
# Generated by docker-release for HAProxy
backend app_be
    balance roundrobin
    option http-keep-alive
    http-reuse safe
    server s0 172.18.0.4:80 check
    server s1 172.18.0.5:80 check
```

---

### Traefik

Writes a dynamic configuration YAML to a shared volume. Traefik's file provider picks it up automatically — no reload or exec needed.

Routing rules live on the **app service labels**, not in a separate config file. The `traefik.http.routers.*` labels tell Traefik which rule matches and which service to use; `docker-release` keeps the upstream server list in the dynamic file in sync.

> See a full working example in [`tests/traefik/`](tests/traefik/).

```yaml
services:
  docker-release:
    image: your-registry/docker-release:latest
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
      - traefik-config:/shared/traefik-config:rw

  traefik:
    image: traefik:v3
    ports:
      - "80:80"
      - "8080:8080"   # dashboard
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
    deploy:
      replicas: 2
    labels:
      # docker-release
      release.enable: "true"
      release.provider: traefik
      release.strategy: linear
      release.traefik.config_dir: /shared/traefik-config
      # Traefik routing
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

The key label is `traefik.http.routers.app.service: "app@file"` — the `@file` suffix tells Traefik to look up the `app` service definition in the dynamic file provider (where `docker-release` writes the upstream list) rather than via Docker discovery.

---

### nginx-proxy

For stacks using [`nginx-proxy`](https://github.com/nginx-proxy/nginx-proxy). Routing is configured entirely via environment variables on the app container — no config files to maintain. `docker-release` manages the container lifecycle; `nginx-proxy` reacts to Docker events automatically.

> See a full working example in [`tests/nginx-proxy/`](tests/nginx-proxy/).

```yaml
services:
  docker-release:
    image: your-registry/docker-release:latest
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock

  nginx-proxy:
    image: nginxproxy/nginx-proxy:alpine
    ports:
      - "80:80"
    volumes:
      - /var/run/docker.sock:/tmp/docker.sock:ro

  app:
    image: your-registry/app:latest
    deploy:
      replicas: 2
    environment:
      VIRTUAL_HOST: app.example.com   # hostname nginx-proxy listens on
      VIRTUAL_PATH: /app/             # path prefix to match
      VIRTUAL_DEST: /                 # rewrite destination (strips the prefix)
    labels:
      release.enable: "true"
      release.provider: nginx-proxy
      release.strategy: linear
    healthcheck:
      test: ["CMD", "wget", "-qO-", "http://localhost/health"]
      interval: 10s
      timeout: 5s
      retries: 3
```

Key environment variables:

| Variable | Description |
|----------|-------------|
| `VIRTUAL_HOST` | Hostname nginx-proxy generates a server block for |
| `VIRTUAL_PATH` | URL path prefix to match (include trailing slash) |
| `VIRTUAL_DEST` | Upstream path after stripping the prefix (`/` to proxy to root) |
| `VIRTUAL_PORT` | Container port to proxy to (defaults to the first exposed port) |

---

### No proxy (worker / job rollout)

For services with no network exposure — background workers, job runners, sidecars — use `provider: none`. `docker-release` handles rolling replacement gated purely on container health. No LB plumbing required.

`canary` and `blue-green` require an LB to split traffic and are rejected with an error when used with `provider: none`.

> See a full working example in [`tests/none/`](tests/none/).

```yaml
services:
  docker-release:
    image: your-registry/docker-release:latest
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock

  worker:
    image: your-registry/worker:latest
    deploy:
      replicas: 3
    labels:
      release.enable: "true"
      release.provider: none
      release.strategy: linear
      release.health_check_timeout: 60s
      release.drain_timeout: 5s
    healthcheck:
      test: ["CMD", "my-worker", "--health"]
      interval: 10s
      timeout: 5s
      retries: 3
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
release.affinity: ip
```

### Canary

Routes a small percentage of traffic to the new version and gradually increases it. Rolls back automatically if the canary becomes unhealthy.

```yaml
release.strategy: canary
release.canary.start_percentage: 10
release.canary.step: 20
release.canary.interval: 2m
release.affinity: ip    # ip | cookie
```

---

## Label reference

### App services

| Label | Default | Description |
|-------|---------|-------------|
| `release.enable` | — | Set `true` to manage this service |
| `release.provider` | `nginx-proxy` | `nginx` \| `angie` \| `caddy` \| `haproxy` \| `traefik` \| `nginx-proxy` \| `none` |
| `release.strategy` | `linear` | `linear` \| `blue-green` \| `canary` |
| `release.health_check_timeout` | `60s` | Max wait for container to become healthy |
| `release.drain_timeout` | `10s` | Time to wait after removing from upstream before stopping |
| `release.upstream` | _(service name)_ | Override the upstream block name |
| `release.affinity` | — | Session affinity: `ip` or `cookie` (canary and blue-green) |
| `release.nginx.container` | — | Nginx service name (for reload) |
| `release.nginx.config_dir` | — | Shared config volume path inside docker-release |
| `release.nginx.keepalive` | _(auto)_ | Nginx keepalive connections per upstream |
| `release.angie.container` | — | Angie service name |
| `release.angie.config_dir` | — | Shared config volume path inside docker-release |
| `release.angie.keepalive` | _(auto)_ | Angie keepalive connections per upstream |
| `release.caddy.container` | — | Caddy service name |
| `release.caddy.config_dir` | — | Shared config volume path inside docker-release |
| `release.caddy.path` | — | URL path prefix (e.g. `/app`); generates `handle_path` block |
| `release.caddy.keepalive` | _(auto)_ | Caddy keepalive idle conns per host |
| `release.haproxy.container` | — | HAProxy service name |
| `release.haproxy.config_dir` | — | Shared config volume path inside docker-release |
| `release.traefik.config_dir` | — | Shared config volume path inside docker-release |
| `release.bg.soak_time` | `5m` | Blue/Green: hold old containers this long before removal |
| `release.bg.green_weight` | `50` | Blue/Green: traffic percentage to green during soak |
| `release.canary.start_percentage` | `10` | Canary: initial traffic percentage |
| `release.canary.step` | `20` | Canary: percentage increase per interval |
| `release.canary.interval` | `2m` | Canary: time between steps |

Use Docker-native `healthcheck:` on app services. `docker-release` waits for Docker's `healthy` status and listens for Docker health events from the socket.

---

## Local development

Once the stack is running (`docker compose up -d`), run `make dev` to symlink the plugin script into your Docker CLI plugins directory. No Go toolchain needed — the binary lives inside Docker.

```sh
make dev   # symlinks scripts/docker-release → ~/.docker/cli-plugins/docker-release
```
