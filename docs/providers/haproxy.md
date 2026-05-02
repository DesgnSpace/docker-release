# HAProxy Provider

Use this when your app runs behind HAProxy.

`docker-release` writes backend files to a shared volume, then reloads HAProxy.

## Compose Example

```yaml
services:
  docker-release:
    image: malico/docker-release:0.1
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
    labels:
      release.enable: "true"
      release.provider: haproxy
      release.haproxy.service: haproxy
      release.haproxy.config_dir: /shared/haproxy-config
    healthcheck:
      test: ["CMD", "wget", "-qO-", "http://localhost/health"]
      interval: 10s
      timeout: 5s
      retries: 3

volumes:
  haproxy-config:
```

## HAProxy Config

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

## Required Labels

```yaml
release.enable: "true"
release.provider: haproxy
release.haproxy.service: haproxy
release.haproxy.config_dir: /shared/haproxy-config
```

## Deploy

```sh
docker compose up -d
docker release app
```

## Notes

- HAProxy must run with `-W` for master-worker mode.
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

```haproxy
frontend http
    bind *:80

    acl is_app path_beg /app
    acl is_api path_beg /api

    use_backend app_be if is_app
    use_backend api_be if is_api
```

`docker-release` writes `app_be` and `api_be` backends.

## Strip a Path Prefix

Use this when your app expects `/`, not `/app`.

```haproxy
frontend http
    bind *:80

    acl is_app path_beg /app
    http-request set-path %[path,regsub(^/app,/)] if is_app
    use_backend app_be if is_app
```

## Common Problems

| Problem | Fix |
|---|---|
| Reload fails | Run HAProxy with `-W`. |
| HAProxy cannot load backends | Include `/etc/haproxy/conf.d` in the command. |
| App receives `/app` in the path | Add a path-strip rule in `haproxy.cfg`. |
