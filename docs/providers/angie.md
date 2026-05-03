# Angie Provider

Use this when your app runs behind Angie.

`docker-release` writes upstream files to a shared volume, then reloads Angie.

## When to Use This

Use this provider when you run Angie as your reverse proxy and can add an `include` line to its config.

Angie is close to Nginx, but many Angie images use `/etc/angie/http.d` instead of `/etc/nginx/conf.d`.

## What Gets Written

For an app named `app`, `docker-release` writes a file like this:

```nginx
upstream app_upstream {
    sticky cookie _srr_a172cedcae path=/;
    server 172.18.0.4:80;
    server 172.18.0.5:80;
}
```

Your Angie config chooses where that upstream is used.

## Compose Example

```yaml
services:
  docker-release:
    image: malico/docker-release:0.1
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
      - angie-config:/shared/angie-config:rw # docker-release writes Angie upstream files here

  angie:
    image: docker.angie.software/angie:latest
    ports:
      - "80:80"
    volumes:
      - angie-config:/etc/angie/http.d/custom:ro # Angie reads generated upstream files here
      - ./angie.conf:/etc/angie/http.d/default.conf:ro # Your base Angie routes

  app:
    image: your-registry/app:latest
    labels:
      release.enable: "true"
      release.provider: angie
      release.angie.service: angie
      release.angie.config_dir: /shared/angie-config
    healthcheck:
      test: ["CMD", "wget", "-qO-", "http://localhost/health"]
      interval: 10s
      timeout: 5s
      retries: 3

volumes:
  angie-config:
```

## Angie Config

```nginx
include /etc/angie/http.d/custom/*.conf;

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
release.provider: angie
release.angie.service: angie
release.angie.config_dir: /shared/angie-config
```

## What Each Label Means

| Label | Meaning |
|---|---|
| `release.enable` | Allows `docker-release` to manage this app. |
| `release.provider` | Selects the Angie provider. |
| `release.angie.service` | Name of the Angie Compose service to reload. |
| `release.angie.config_dir` | Path where `docker-release` writes generated files. |

## Deploy

```sh
docker compose up -d
docker release app
```

## Notes

- Angie uses `http.d` in many images, not `conf.d`.
- Cookie affinity uses a generated cookie name like `_srr_a172cedcae`.

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

```nginx
include /etc/angie/http.d/custom/*.conf;

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

## Common Problems

| Problem | Fix |
|---|---|
| Angie cannot find generated files | Check the mount path. Many Angie images use `/etc/angie/http.d`. |
| Reload does not run | Set `release.angie.service` to your Angie Compose service name. |
| Sticky sessions do not work | Use `release.affinity: cookie`. |
