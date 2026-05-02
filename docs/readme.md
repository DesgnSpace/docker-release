# docker-release Docs

Use `docker-release` to deploy Docker Compose services without planned downtime.

This page gives the short version. Provider guides hold the full setup examples.

## Pick a Provider

| Provider | Use it when | Guide |
|---|---|---|
| `nginx` | You use Nginx. | [Nginx](./providers/nginx.md) |
| `angie` | You use Angie. | [Angie](./providers/angie.md) |
| `caddy` | You use Caddy. | [Caddy](./providers/caddy.md) |
| `haproxy` | You use HAProxy. | [HAProxy](./providers/haproxy.md) |
| `traefik` | You use Traefik. | [Traefik](./providers/traefik.md) |
| `nginx-proxy` | You use `nginxproxy/nginx-proxy`. | [nginx-proxy](./providers/nginx-proxy.md) |
| `none` | You deploy workers or jobs. | [No proxy](./providers/none.md) |

## Install CLI

```sh
curl -fsSL https://raw.githubusercontent.com/desgnspace/docker-release/main/scripts/docker-release \
  | sudo tee ~/.docker/cli-plugins/docker-release >/dev/null \
  && sudo chmod +x ~/.docker/cli-plugins/docker-release
```

## Commands

```sh
docker release app           # deploy app
docker release app --force   # deploy even if one is running
docker release status        # show all services
docker release status app    # show one service
docker release rollback app  # roll back app
```

If a service name is also a command, use the long form:

```sh
docker release release status
```

## Deploy Flow

1. Start new containers.
2. Wait for health checks.
3. Add new containers to the proxy.
4. Drain old containers.
5. Stop old containers.

## Strategies

### Linear

Replaces containers one by one.

```yaml
release.strategy: linear
```

### Blue/Green

Starts a full new set, moves traffic, then keeps the old set for rollback.

```yaml
release.strategy: blue-green
release.bg.soak_time: 5m
release.bg.green_weight: 50
release.affinity: ip
```

### Canary

Sends some traffic to the new version, then sends more over time.

```yaml
release.strategy: canary
release.canary.start_percentage: 10
release.canary.step: 20
release.canary.interval: 2m
release.affinity: ip
```

## Common Labels

| Label | Default | Use |
|---|---|---|
| `release.enable` | — | Set to `true` to manage this service. |
| `release.provider` | `nginx-proxy` | Proxy provider. |
| `release.strategy` | `linear` | Deploy style. |
| `release.health_check_timeout` | `60s` | Max wait for a healthy container. |
| `release.drain_timeout` | `10s` | Wait time before old containers stop. |
| `release.upstream` | service name | Custom upstream name. |
| `release.affinity` | `cookie` | Session affinity: `cookie`, `ip`, or empty. |

## Provider Labels

| Label | Use |
|---|---|
| `release.nginx.service` | Nginx Compose service name. |
| `release.nginx.config_dir` | Shared Nginx config path. |
| `release.angie.service` | Angie Compose service name. |
| `release.angie.config_dir` | Shared Angie config path. |
| `release.caddy.service` | Caddy Compose service name. |
| `release.caddy.config_dir` | Shared Caddy config path. |
| `release.caddy.path` | URL path for Caddy, such as `/app`. |
| `release.haproxy.service` | HAProxy Compose service name. |
| `release.haproxy.config_dir` | Shared HAProxy config path. |
| `release.traefik.config_dir` | Shared Traefik config path. |

## Health Checks

Add a Docker `healthcheck` to each app service. `docker-release` waits for `healthy` before it sends traffic to a new container.

## State

Mount `/var/lib/docker-release` to a volume. This keeps state for rollback.

```yaml
volumes:
  - docker-release-state:/var/lib/docker-release
```
