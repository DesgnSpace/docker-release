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

## Deploy

```sh
docker compose up -d
docker release app
```

## Notes

- `docker-release` always writes a named snippet. Import it inside your Caddy site block wherever you need it.
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

Use `handle_path` in your Caddyfile to route by URL path. `docker-release` always writes a named snippet — you control where and how it is used.

Route `/app/*` to `app` (Caddy strips the prefix before forwarding):

```caddy
handle_path /app/* {
    import app_upstream
}
```

Route `/api/*` to `api`:

```caddy
handle_path /api/* {
    import api_upstream
}
```

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
