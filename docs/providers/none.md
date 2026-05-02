# No Proxy Provider

Use this for workers, jobs, and services that do not receive web traffic.

`docker-release` replaces containers and waits for health checks. It does not write proxy config.

## Compose Example

```yaml
services:
  docker-release:
    image: malico/docker-release:0.1
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
      - docker-release-state:/var/lib/docker-release

  worker:
    image: your-registry/worker:latest
    labels:
      release.enable: "true"
      release.provider: none
      release.strategy: linear
      release.health_check_timeout: 60s
      release.drain_timeout: 5s
    healthcheck:
      test: ["CMD", "my-worker", "--health"]
      interval: 10s
      timeout: 5s
      retries: 3

volumes:
  docker-release-state:
```

## Required Labels

```yaml
release.enable: "true"
release.provider: none
release.strategy: linear
```

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
release.strategy: linear
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
      release.strategy: linear
```

## Common Problems

| Problem | Fix |
|---|---|
| Canary is rejected | Use a proxy provider, or switch to `linear`. |
| Rollback state is lost | Mount `/var/lib/docker-release` to a volume. |
| Deploy waits too long | Check the service health check. |
