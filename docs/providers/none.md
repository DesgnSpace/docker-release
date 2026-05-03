# No Proxy Provider

Use this for workers, jobs, and services that do not receive web traffic.

`docker-release` replaces containers and waits for health checks. It does not write proxy config.

## When to Use This

Use this provider for services that do work in the background and do not receive web traffic.

Good examples:

- queue workers
- schedulers
- cron jobs
- sidecars

Do not use this provider for web apps that need canary or blue/green traffic splitting.

## Compose Example

```yaml
services:
  docker-release:
    image: malico/docker-release:0.1
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock

  worker:
    image: your-registry/worker:latest
    labels:
      release.enable: "true"
      release.provider: none
      release.health_check_timeout: 60s
      release.drain_timeout: 5s
    healthcheck:
      test: ["CMD", "my-worker", "--health"]
      interval: 10s
      timeout: 5s
      retries: 3

```

## Required Labels

```yaml
release.enable: "true"
release.provider: none
```

## What Each Label Means

| Label | Meaning |
|---|---|
| `release.enable` | Allows `docker-release` to manage this service. |
| `release.provider` | Set to `none` so no proxy config is written. |

## Deploy

```sh
docker compose up -d
docker release worker
```

## Notes

- Use only `linear` with this provider.
- `canary` and `blue-green` need a proxy because they split traffic.

## Linear Example

```yaml
release.enable: "true"
release.provider: none
release.health_check_timeout: 60s
release.drain_timeout: 5s
```

## Worker Health Check Example

```yaml
healthcheck:
  test: ["CMD", "my-worker", "--health"]
  interval: 10s
  timeout: 5s
  retries: 3
```

## Cron Job Example

```yaml
services:
  job:
    image: your-registry/job:latest
    labels:
      release.enable: "true"
      release.provider: none
```

## Common Problems

| Problem | Fix |
|---|---|
| Canary is rejected | Use a proxy provider, or switch to `linear`. |
| Rollback state is lost | Mount `/var/lib/docker-release` to a volume. |
| Deploy waits too long | Check the service health check. |
