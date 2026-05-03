# nginx-proxy Provider

Use this when your stack uses `nginxproxy/nginx-proxy`.

`docker-release` updates the `nginx-proxy` template. `nginx-proxy` reloads from Docker events.

## When to Use This

Use this provider only with `nginxproxy/nginx-proxy`.

Your app routing still uses `VIRTUAL_HOST`, `VIRTUAL_PATH`, and related environment variables. `docker-release` updates the template so the active container list matches the deploy state.

## What Gets Written

`docker-release` writes a managed `nginx.tmpl` file. `nginx-proxy` uses that template to render Nginx config.

The template keeps normal `nginx-proxy` behavior for services that `docker-release` does not manage.

## Compose Example

```yaml
services:
  docker-release:
    image: malico/docker-release:0.1
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
      - nginx-tmpl:/shared/nginx-tmpl:rw # docker-release writes nginx.tmpl here

  nginx-proxy:
    image: nginxproxy/nginx-proxy:alpine
    ports:
      - "80:80"
    volumes:
      - /var/run/docker.sock:/tmp/docker.sock:ro
      - nginx-tmpl:/app/custom:ro # nginx-proxy reads nginx.tmpl here
    environment:
      NGINX_TMPL: /app/custom/nginx.tmpl

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

## What Each Label Means

| Label | Meaning |
|---|---|
| `release.enable` | Allows `docker-release` to manage this app. |
| `release.provider` | Selects the nginx-proxy provider. |
| `release.nginx.config_dir` | Folder where `docker-release` writes `nginx.tmpl`. |

## What Each Environment Variable Means

| Variable | Meaning |
|---|---|
| `VIRTUAL_HOST` | Host name that nginx-proxy listens on. |
| `VIRTUAL_PATH` | URL path for the app. Use a trailing slash, such as `/app/`. |
| `VIRTUAL_DEST` | Path sent to the app after routing. Use `/` to strip the prefix. |
| `VIRTUAL_PORT` | App port to proxy to when the image exposes more than one port. |

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
