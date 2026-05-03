# Caddy Provider

Use this when your app runs behind Caddy.

`docker-release` writes `.caddy` files to a shared volume, then reloads Caddy.

## When to Use This

Use this provider when Caddy is your reverse proxy and you want `docker-release` to keep upstream backends current while your Caddyfile owns routes, headers, auth, and other directives.

## What Gets Written

By default, `docker-release` writes a named snippet:

```caddy
(app_upstream) {
    reverse_proxy 172.18.0.4:80 172.18.0.5:80 {
        lb_policy cookie _srr_a172cedcae
    }
}
```

Import the generated file globally, then use the snippet inside your site block.

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

Most sites proxy the whole domain to one app. Use this first.

```caddy
import /etc/caddy/conf.d/*.caddy

example.com {
    encode gzip zstd
    header X-Frame-Options DENY

    import app_upstream
}
```

For local testing, replace `example.com` with `:80`.

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
| `release.caddy.path` | empty | path mode for one app under a path, such as `/app` |

## Deploy

```sh
docker compose up -d
docker release app
```

## Notes

- No `release.caddy.path` means whole-site mode. Import `<service>_upstream` inside your own Caddy site block.
- Set `release.caddy.path` only when one domain serves many apps by path, such as `/app` and `/api`.
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

## Path Mode (Optional)

Use path mode only when one Caddy site serves more than one app by URL path.

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

Keep your own routes in `Caddyfile`. Put generated snippet imports at the top level, then use the named upstream as the fallback app route.

```caddy
import /etc/caddy/conf.d/*.caddy

example.com {
    encode gzip zstd

    handle /static/* {
        root * /srv/www
        file_server
    }

    handle /health {
        respond 200
    }

    handle {
        import app_upstream
    }
}
```

## Common Problems

| Problem | Fix |
|---|---|
| Caddy does not route to the app | Check `import /etc/caddy/conf.d/*.caddy`. |
| App receives the wrong path | Remove `release.caddy.path` for whole-site routing, or check the path value. |
| Reload does not run | Set `release.caddy.service` to your Caddy Compose service name. |
