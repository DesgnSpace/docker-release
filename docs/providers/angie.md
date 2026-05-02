# Angie Provider

Use this when your app runs behind Angie.

`docker-release` writes upstream files to a shared volume, then reloads Angie.

## Compose Example

```yaml
services:
  docker-release:
    image: malico/docker-release:0.1
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
      - docker-release-state:/var/lib/docker-release
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
    labels:
      release.enable: "true"
      release.provider: angie
      release.strategy: linear
      release.angie.service: angie
      release.angie.config_dir: /shared/angie-config
    healthcheck:
      test: ["CMD", "wget", "-qO-", "http://localhost/health"]
      interval: 10s
      timeout: 5s
      retries: 3

volumes:
  docker-release-state:
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
