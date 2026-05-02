# Nginx provider

Use this setup to run docker-release with a single app behind Nginx in a production-like stack.

## Compose wiring (minimal)

```yaml
# docker-compose.yml
services:
  docker-release:
    image: your-registry/docker-release:latest
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
      - nginx-config:/shared/nginx-config:rw
    command: ["watch"]

  nginx:
    image: nginx:alpine
    ports:
      - "8081:80"
    volumes:
      - nginx-config:/etc/nginx/conf.d/custom:ro
      - ./nginx.conf:/etc/nginx/conf.d/default.conf:ro
    depends_on:
      docker-release:
        condition: service_started

  web_app:
    image: your-registry/web_app:latest
    labels:
      - "release.enable=true"
      - "release.provider=nginx"
      - "release.strategy=linear"
      - "release.nginx.container=nginx"
      - "release.nginx.config_dir=/shared/nginx-config"
    healthcheck:
      test: ["CMD", "wget", "-qO-", "http://localhost/health"]
      interval: 10s
      timeout: 5s
      retries: 3

volumes:
  nginx-config:
```

- `docker-release` needs the Docker socket and a shared volume (`nginx-config`).
- `nginx` mounts the same volume read-only plus a base config.
- `web_app` is a single managed service using the linear strategy (typical production default).

## Base Nginx config

```nginx
# nginx.conf
server {
    listen 80;

    proxy_set_header Host $host;
    proxy_set_header X-Real-IP $remote_addr;

    location / {
        proxy_pass http://web_app_upstream/;
    }
}
```

`docker-release` writes upstream definitions into `/shared/nginx-config`; Nginx reads them from `/etc/nginx/conf.d/custom` via the shared volume. By default, the upstream name is derived from the service (e.g., `web_app` → `web_app_upstream`).

## Required labels (per service)

```yaml
labels:
  - "release.enable=true"
  - "release.provider=nginx"
  - "release.nginx.container=nginx"                 # Nginx service name
  - "release.nginx.config_dir=/shared/nginx-config" # Shared mount inside docker-release
```

## Strategy labels (add when needed)

```yaml
# Linear (default)
- "release.strategy=linear"
- "release.drain_timeout=5s"
- "release.health_check_timeout=20s"

# Canary
- "release.strategy=canary"
- "release.canary.start_percentage=25"
- "release.canary.step=25"
- "release.canary.interval=10s"

# Blue/Green
- "release.strategy=blue-green"
- "release.bg.soak_time=30s"
- "release.bg.green_weight=50"
- "release.bg.affinity=ip"
```

## Health checks

Use Docker-native `healthcheck:` on app services. `docker-release` waits for Docker's `healthy` status and listens for Docker health events from the socket.

## How to run

1) Place `docker-compose.yml` and `nginx.conf` alongside your app (for example, in the repo root).
2) Start the stack: `docker compose up --build`.
3) Open `http://localhost:8081/` to reach `web_app` (adjust for your host/paths).
4) When a new image is available, trigger a release: `docker compose exec docker-release docker-release release --service web_app`.
