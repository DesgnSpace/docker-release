# Caddy Provider

Use this when your app runs behind Caddy.

`docker-release` writes `.caddy` files to a shared volume, then reloads Caddy.

## When to Use This

Use this provider when Caddy is your reverse proxy and you want `docker-release` to create `reverse_proxy` blocks for your apps.

This is a good fit if your apps use path routes like `/app` or `/api`.

## What Gets Written

For `release.caddy.path: /app`, `docker-release` writes a file like this:

```caddy
handle_path /app* {
    reverse_proxy 172.18.0.4:80 172.18.0.5:80 {
        lb_policy cookie _srr_a172cedcae
    }
}
```

`handle_path` removes `/app` before the request reaches your app.

## Compose Example

```yaml
services:
  docker-release:
    image: malico/docker-release:0.1
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
      - caddy-config:/shared/caddy-config:rw # docker-release writes Caddy files here

  caddy:
    image: caddy:alpine
    ports:
      - "80:80"
    volumes:
      - caddy-config:/etc/caddy/conf.d:ro # Caddy reads generated files here
      - ./Caddyfile:/etc/caddy/Caddyfile:ro # Your base Caddy config

  app:
    image: your-registry/app:latest
    labels:
      release.enable: "true"
      release.provider: caddy
    healthcheck:
      test: ["CMD", "wget", "-qO-", "http://localhost/health"]
      interval: 10s
      timeout: 5s
      retries: 3

volumes:
  caddy-config:
```

## Caddyfile

```caddy
:80 {
    encode gzip zstd
    import /etc/caddy/conf.d/*.caddy
}
```

## Required Labels

```yaml
release.enable: "true"
release.provider: caddy
```

## Optional Overrides

| Label | Default | Override when |
|---|---|---|
| `release.caddy.service` | auto-detected by image | multiple Caddy containers in the project |
| `release.caddy.config_dir` | `/shared/caddy-config` | shared volume mounted at a different path |
| `release.caddy.path` | `/<service-name>` | URL path differs from the service name; set to empty for named-snippet mode |

## Deploy

```sh
docker compose up -d
docker release app
```

## Notes

- `release.caddy.path` sets the URL path, such as `/app`.
- Caddy removes that path before the request reaches your app.
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
```

## Path Examples

Route `/app/*` to `app`:

```yaml
release.caddy.path: /app
```

Route `/api/*` to `api`:

```yaml
release.caddy.path: /api
```

Caddy receives `/app/users`, then sends `/users` to the app.

## Add Static Routes

Keep your own routes in `Caddyfile`. Put the import after shared config.

```caddy
:80 {
    encode gzip zstd

    respond /ping 200

    handle /static/* {
        root * /srv/www
        file_server
    }

    import /etc/caddy/conf.d/*.caddy
}
```

## Common Problems

| Problem | Fix |
|---|---|
| Caddy does not route to the app | Check `import /etc/caddy/conf.d/*.caddy`. |
| App receives the wrong path | Check `release.caddy.path`. |
| Reload does not run | Set `release.caddy.service` to your Caddy Compose service name. |
