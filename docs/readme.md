# docker-release Docs

Use `docker-release` to deploy Docker Compose services without planned downtime.

This page gives the short version. Provider guides hold the full setup examples.

## What You Need

Every setup needs three parts:

| Part | Why it is needed |
|---|---|
| `docker-release` service | Watches Docker and runs deploys. |
| Docker socket mount | Lets `docker-release` start, stop, and inspect containers. |
| Managed app labels | Tell `docker-release` which services it can deploy. |

Most web apps also need a proxy provider. The proxy still serves traffic. `docker-release` only writes config for it.

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

## How Config Moves

For file-based providers, `docker-release` and the proxy share a Docker volume.

Example with Nginx:

```yaml
docker-release:
  volumes:
    - nginx-config:/shared/nginx-config:rw # writes generated upstream files

nginx:
  volumes:
    - nginx-config:/etc/nginx/conf.d/custom:ro # reads generated upstream files
```

The path is different for each provider. Use the provider guide for the exact mount.

## Strategies

### Linear

Replaces containers one by one.

```yaml
# No label needed. Linear is the default.
```

### Blue/Green

Starts a full new set, moves traffic, then keeps the old set for rollback.

```yaml
release.strategy: blue-green
release.bg.soak_time: 5m
release.bg.green_weight: 50
```

### Canary

Sends some traffic to the new version, then sends more over time.

```yaml
release.strategy: canary
release.canary.start_percentage: 10
release.canary.step: 20
release.canary.interval: 2m
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
| `release.affinity` | `ip` | Session affinity: `ip`, `cookie`, or empty. |

## Session Affinity

`release.affinity` controls how requests are pinned to a backend during a deployment. This keeps users on the same container while old and new versions run side by side.

| Value | Behavior |
|---|---|
| `ip` (default) | Routes by client IP. All providers support this. |
| `cookie` | Routes by sticky cookie. **Not supported by Nginx or nginx-proxy** — both fall back to IP hashing. Use `cookie` only with Angie, Caddy, HAProxy, or Traefik. |
| `""` (empty) | No affinity. Requests are load-balanced freely. |

**Nginx and nginx-proxy note:** Nginx OSS has no sticky cookie module. Setting `release.affinity: cookie` has no extra effect — both `ip` and `cookie` produce `ip_hash` in the generated upstream block. If you need real cookie-based sticky sessions, use Angie, Caddy, HAProxy, or Traefik.

## Provider Labels

All provider labels are optional. Defaults work for standard single-proxy setups.

| Label | Default | Use |
|---|---|---|
| `release.nginx.service` | auto-detected by image | Nginx Compose service name. |
| `release.nginx.config_dir` | `/shared/nginx-config` | Shared Nginx config path. |
| `release.angie.service` | auto-detected by image | Angie Compose service name. |
| `release.angie.config_dir` | `/shared/angie-config` | Shared Angie config path. |
| `release.caddy.service` | auto-detected by image | Caddy Compose service name. |
| `release.caddy.config_dir` | `/shared/caddy-config` | Shared Caddy config path. |
| `release.caddy.path` | empty | Optional path mode for Caddy, such as `/app`. Leave empty for a whole site. |
| `release.haproxy.service` | auto-detected by image | HAProxy Compose service name. |
| `release.haproxy.config_dir` | `/shared/haproxy-config` | Shared HAProxy config path. |
| `release.traefik.config_dir` | `/shared/traefik-config` | Shared Traefik config path. |

## Health Checks

Add a Docker `healthcheck` to each app service. `docker-release` waits for `healthy` before it sends traffic to a new container.

If a service has no health check, Docker may not report a useful `healthy` state. Add a small endpoint or command that proves the app can serve work.

Example:

```yaml
healthcheck:
  test: ["CMD", "wget", "-qO-", "http://localhost/health"]
  interval: 10s
  timeout: 5s
  retries: 3
```

## Optional Rollback State

Basic examples do not need this volume.

Add it if you want rollback state to survive when the `docker-release` container restarts.

```yaml
services:
  docker-release:
    volumes:
      - docker-release-state:/var/lib/docker-release

volumes:
  docker-release-state:
```

## Safe Defaults

| Setting | Default | Why it is safe |
|---|---|---|
| Strategy | `linear` | Replaces one container at a time. |
| Affinity | `ip` | Keeps users on one backend during a deployment. |
| Drain timeout | `10s` | Gives old requests time to finish. |
| Health timeout | `60s` | Gives new containers time to become ready. |

## Next Step

Open the provider guide for your proxy and copy the full example.
