# Nginx Provider

Use this when your app runs behind Nginx.

`docker-release` writes upstream files to a shared volume, then reloads Nginx.

## Compose Example

```yaml
services:
  docker-release:
    image: malico/docker-release:0.1
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
      - nginx-config:/shared/nginx-config:rw # docker-release writes Nginx upstream files here

  nginx:
    image: nginx:alpine
    ports:
      - "80:80"
    volumes:
      - nginx-config:/etc/nginx/conf.d/custom:ro # Nginx reads generated upstream files here
      - ./nginx.conf:/etc/nginx/conf.d/default.conf:ro # Your base Nginx routes

  app:
    image: your-registry/app:latest
    labels:
      release.enable: "true"
      release.provider: nginx
      release.nginx.service: nginx
      release.nginx.config_dir: /shared/nginx-config
    healthcheck:
      test: ["CMD", "wget", "-qO-", "http://localhost/health"]
      interval: 10s
      timeout: 5s
      retries: 3

volumes:
  nginx-config:
```

## Nginx Config

```nginx
include /etc/nginx/conf.d/custom/*.conf;

server {
    listen 80;

    proxy_set_header Host $http_host;
    proxy_set_header X-Real-IP $remote_addr;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto $scheme;

    location / {
        proxy_pass http://app_upstream/;
    }
}
```

## Required Labels

```yaml
release.enable: "true"
release.provider: nginx
release.nginx.service: nginx
release.nginx.config_dir: /shared/nginx-config
```

## Deploy

```sh
docker compose up -d
docker release app
```

## Notes

- `release.nginx.service` is the Compose service name for Nginx.
- If you do not set it, `docker-release` tries to find a running Nginx container in the same Compose project.
- Nginx open source uses `ip_hash` for sticky traffic. It does not set sticky cookies.

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
release.affinity: ip
```

## Multiple Apps

Add one `location` per app. Each app gets its own upstream name.

```nginx
include /etc/nginx/conf.d/custom/*.conf;

server {
    listen 80;

    location /app/ {
        proxy_pass http://app_upstream/;
    }

    location /api/ {
        proxy_pass http://api_upstream/;
    }
}
```

`api` labels:

```yaml
release.enable: "true"
release.provider: nginx
release.nginx.service: nginx
release.nginx.config_dir: /shared/nginx-config
```

## Common Problems

| Problem | Fix |
|---|---|
| Nginx does not see new servers | Check `include /etc/nginx/conf.d/custom/*.conf;`. |
| Reload does not run | Set `release.nginx.service` to your Nginx Compose service name. |
| Rollback state is lost | Mount `/var/lib/docker-release` to a volume. |
