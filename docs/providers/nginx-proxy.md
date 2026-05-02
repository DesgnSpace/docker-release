# nginx-proxy Provider

Use this when your stack uses `nginxproxy/nginx-proxy`.

`docker-release` updates the `nginx-proxy` template. `nginx-proxy` reloads from Docker events.

## Compose Example

```yaml
services:
  docker-release:
    image: malico/docker-release:0.1
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
      - nginx-tmpl:/shared/nginx-tmpl:rw

  nginx-proxy:
    image: nginxproxy/nginx-proxy:alpine
    ports:
      - "80:80"
    volumes:
      - /var/run/docker.sock:/tmp/docker.sock:ro
      - nginx-tmpl:/etc/docker-gen/templates:ro

  app:
    image: your-registry/app:latest
    environment:
      VIRTUAL_HOST: app.example.com
      VIRTUAL_PATH: /app/
      VIRTUAL_DEST: /
    labels:
      release.enable: "true"
      release.provider: nginx-proxy
      release.nginx.config_dir: /shared/nginx-tmpl
    healthcheck:
      test: ["CMD", "wget", "-qO-", "http://localhost/health"]
      interval: 10s
      timeout: 5s
      retries: 3

volumes:
  nginx-tmpl:
```

## Required Labels

```yaml
release.enable: "true"
release.provider: nginx-proxy
release.nginx.config_dir: /shared/nginx-tmpl
```

## Required Environment

```yaml
VIRTUAL_HOST: app.example.com
VIRTUAL_PATH: /app/
VIRTUAL_DEST: /
```

## Deploy

```sh
docker compose up -d
docker release app
```

## Notes

- `VIRTUAL_HOST` sets the host name.
- `VIRTUAL_PATH` sets the path.
- `VIRTUAL_DEST` sets the path sent to your app.
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

## Host Example

```yaml
environment:
  VIRTUAL_HOST: app.example.com
  VIRTUAL_PORT: 80
```

## Path Example

```yaml
environment:
  VIRTUAL_HOST: app.example.com
  VIRTUAL_PATH: /app/
  VIRTUAL_DEST: /
```

`/app/users` is sent to your app as `/users`.

## Multiple Apps

```yaml
app:
  environment:
    VIRTUAL_HOST: example.com
    VIRTUAL_PATH: /app/

api:
  environment:
    VIRTUAL_HOST: example.com
    VIRTUAL_PATH: /api/
```

## Common Problems

| Problem | Fix |
|---|---|
| Route does not appear | Check `VIRTUAL_HOST`. |
| Path routing fails | Include the trailing slash in `VIRTUAL_PATH`, such as `/app/`. |
| Template is not updated | Check `release.nginx.config_dir` points to the shared template volume. |
