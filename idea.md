This is a comprehensive technical design document for **`docker-release`**. It merges the conceptual requirements, the architectural constraints (sidecar pattern), and the specific logic for providers and deployment strategies into a single, implementable specification.

---

# Technical Design Document: `docker-release`

**Version:** 1.0.0
**Status:** Draft / Specification
**Scope:** Docker Compose Environments

---

## 1. Executive Summary

`docker-release` is a lightweight, sidecar-based deployment controller designed to bring Kubernetes-grade deployment strategies (Blue/Green, Canary) and zero-downtime guarantees to standard Docker Compose environments.

Unlike `docker-compose up` or existing rollout tools, `docker-release` decouples container lifecycle management from traffic routing. It utilizes a **Provider** abstraction to interface with reverse proxies (Nginx, Traefik), ensuring traffic is drained gracefully from old instances before termination and supporting complex traffic splitting for Canary releases.

---

## 2. Problem Statement

In standard Docker Compose deployments, updating a service creates distinct reliability issues:

1. **Connection Dropping (The "Draining" Problem):** When a container stops, it severs active connections.
2. **Load Balancer Misrouting:** Reverse proxies like `nginx-proxy` often use "Least Connections" load balancing. A shutting-down container drops connections rapidly, falsely appearing as the "least loaded" server. This causes the proxy to route _new_ traffic to the dying container, resulting in 502/504 errors.
3. **Lack of Granularity:** There is no native support for gradual traffic shifting (Canary) or instant atomic switching (Blue/Green) with immediate rollback capabilities.

---

## 3. System Architecture

`docker-release` operates as a "Sidecar Controller." It does not proxy traffic itself; it orchestrates the containers and configures the external proxy that does.

### 3.1 High-Level Component Diagram

```mermaid
graph TD
    User[User / CI pipeline] -->|Trigger Deploy| Controller[docker-release Controller]

    subgraph "Docker Host"
        Controller -->|Mounts| DockerSock((/var/run/docker.sock))
        Controller -->|Writes Config| SharedVol[Shared Config Volume]

        Proxy[Reverse Proxy Provider<br/>(Nginx / Traefik)]
        Proxy -->|Reads Config| SharedVol
        Proxy -->|Routes Traffic| AppV1[App Container V1]
        Proxy -->|Routes Traffic| AppV2[App Container V2]
    end

```

### 3.2 Core Responsibilities

1. **Service Discovery:** Scans for containers with `release.enable=true` labels via the Docker API.
2. **Lifecycle Management:** Spins up new replicas (candidates) without immediately killing old ones.
3. **Traffic Orchestration:** Generates specific configuration files for the chosen **Provider** to shift traffic percentages or drain connections.
4. **State Persistence:** Maintains a `deployment_state.json` to track active vs. candidate containers, enabling deterministic rollbacks.

---

## 4. The Provider Model

To support different load balancers, `docker-release` uses a plugin-like system called **Providers**.

### 4.1 Interface Definition

Every provider must implement:

- `GenerateConfig(state)`: Creates the routing configuration (e.g., `nginx.conf`, `traefik.toml`).
- `Drain(containerID)`: Updates config to mark a specific container as "draining" (stops sending new requests).
- `SetTrafficSplit(percentage)`: Used for Canary deployments.

### 4.2 Supported Providers

#### A. Nginx-Proxy (Primary Target)

- **Mechanism:** `docker-release` generates an `upstream` configuration file located in a volume mounted to `/etc/nginx/conf.d/`.
- **Session Affinity:** Uses `ip_hash` or explicit `cookie` injection to ensure sticky sessions during Canary rollouts.
- **Draining:** Removes the container IP from the `upstream` block _before_ the container is stopped.

#### B. Traefik

- **Mechanism:** Uses the Traefik [File Provider](https://doc.traefik.io/traefik/providers/file/). `docker-release` writes a dynamic YAML file.
- **Benefits:** Traefik watches file changes and hot-reloads instantly without dropping connections.

---

## 5. Deployment Strategies

### 5.1 Linear Deployment (Standard)

- **Goal:** Safe, sequential update with connection draining.
- **Workflow:**

1. Start **1** New Container.
2. Wait for Healthcheck pass.
3. **Provider Action:** Add New Container to LB; Mark **1** Old Container as "Down/Drain".
4. Wait for draining timeout (e.g., 10s).
5. Stop and Remove Old Container.
6. Repeat until all replicas are replaced.

### 5.2 Blue-Green Deployment

- **Goal:** Instant cutover with zero risk.
- **Workflow:**

1. Identify current "Color" (e.g., Blue).
2. Spin up a full set of New Containers (Green).
3. Wait for Healthcheck on all Green containers.
4. **Provider Action:** Switch LB upstream to point **100%** to Green.
5. **Soak Time:** Wait X minutes to monitor stability.
6. **Teardown:** Stop Blue containers.

- **Rollback:** If Green fails during Soak Time, switch LB back to Blue instantly.

### 5.3 Canary Deployment

- **Goal:** Risk mitigation by routing a subset of traffic.
- **Requirement:** **Session Affinity** (Users assigned to Canary must stay on Canary).
- **Workflow:**

1. Start a small subset of New Containers.
2. **Provider Action:** Configure LB to route X% (e.g., 10%) of traffic to New, using a consistent hash (Cookie/IP).
3. **Observation:** Monitor error rates for `release.canary.interval`.
4. **Scale Up:** Increase traffic % and number of containers.
5. **Finalize:** Switch to 100%, drain and stop Old containers.

---

## 6. Configuration & Label Schema

Configuration is handled entirely via Docker Labels on the application service.

| Label Key                         | Value Example | Description                                   |
| --------------------------------- | ------------- | --------------------------------------------- |
| `release.enable`                  | `true`        | Activates the controller for this service.    |
| `release.provider`                | `nginx-proxy` | Selects the traffic manager (nginx, traefik). |
| `release.strategy`                | `canary`      | `linear`                                      |
| `release.health_check_timeout`    | `60s`         | Max time to wait for "healthy" status.        |
| `release.bg.soak_time`            | `5m`          | (Blue-Green) Time to wait before killing old. |
| `release.canary.start_percentage` | `10`          | (Canary) Initial traffic weight.              |
| `release.canary.step`             | `20`          | (Canary) Percentage increase per interval.    |
| `release.canary.interval`         | `2m`          | (Canary) Time between steps.                  |
| `release.canary.affinity`         | `cookie`      | `cookie`                                      |

---

## 7. Example Implementation

Below is a `docker-compose.yml` demonstrating a **Canary** deployment using `nginx-proxy`.

```yaml
version: "3.8"

services:
  # 1. The Controller
  docker-release:
    image: my-org/docker-release:latest
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
      - ./config-share:/etc/nginx/conf.d/custom:rw # Write access
    command: ["watch"]

  # 2. The Provider (Load Balancer)
  nginx-proxy:
    image: nginx:alpine
    ports:
      - "80:80"
    volumes:
      # Mount the config generated by docker-release
      - ./config-share:/etc/nginx/conf.d/custom:ro
      - ./nginx-main.conf:/etc/nginx/nginx.conf:ro

  # 3. The Application
  web-app:
    image: my-org/web-app:v2
    deploy:
      replicas: 4
    labels:
      - "release.enable=true"
      - "release.provider=nginx-proxy"
      - "release.strategy=canary"
      - "release.canary.start_percentage=25"
      - "release.canary.interval=5m"
      - "release.canary.affinity=cookie" # Users get a 'sticky' cookie
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost:8080/health"]
      interval: 10s
      timeout: 5s
      retries: 3
```

## 8. Rollback Logic

The controller maintains a `deployment_state.json` file on its persistent volume.

**State Structure:**

```json
{
  "service": "web-app",
  "status": "in_progress",
  "strategy": "canary",
  "current_weight": 25,
  "active_deployment_id": "deploy_abc123",
  "previous_deployment_id": "deploy_xyz987",
  "containers": {
    "stable": ["container_id_1", "container_id_2"],
    "canary": ["container_id_3"]
  }
}
```

**Rollback Trigger:**

1. **Automatic:** If a New container fails a health check or restarts `X` times during the soak period.
2. **Manual:** User runs `docker exec docker-release rollback web-app`.

**Rollback Action:**

1. Read `deployment_state.json`.
2. Instruct Provider to route 100% traffic back to `stable` containers.
3. Send `SIGTERM` to `canary` containers.
4. Delete `canary` containers.
5. Reset state to `idle`.

## Examples of how we think this will work like

This section details the **proposed** implementation for the three supported providers.

> **⚠️ Engineering Note:** The configurations below are draft specifications. While the logic (weights, upstreams, draining) is solid, the exact syntax of the generated files may evolve during the implementation phase as we refine how the sidecar writes to the shared volumes.

---

## 8. Proposed Provider Implementations

### 8.1 Provider: `nginx-proxy`

**Mechanism:** `docker-release` acts as an intelligent template engine. Instead of `nginx-proxy` auto-discovering containers blindly, `docker-release` generates a specific upstream configuration file in a shared volume that `nginx-proxy` includes.

#### A. The User Configuration (docker-compose.yml)

```yaml
services:
  webapp:
    image: myapp:v2
    labels:
      - "release.enable=true"
      - "release.provider=nginx-proxy"
      - "release.strategy=canary"
      - "release.canary.start_percentage=10"
```

#### B. The Generated Artifact

**File:** `/etc/nginx/conf.d/webapp-upstream.conf` (Mounted in nginx-proxy)

**Scenario:** Canary rollout. `webapp_v1` (Old) is taking 90% traffic, `webapp_v2` (New) is taking 10%. Note the `ip_hash` for session affinity.

```nginx
# Generated by docker-release at 2023-10-27 10:00:00
upstream webapp.local {
    ip_hash; # Sticky sessions for Canary stability

    # OLD Deployment (Active) - Weight 90
    server 172.18.0.5:80 weight=90;
    server 172.18.0.6:80 weight=90;

    # NEW Deployment (Canary) - Weight 10
    server 172.18.0.8:80 weight=10;
}

```

_Note: When draining occurs, `docker-release` simply regenerates this file removing the dying container's IP, reloads Nginx, and THEN kills the container._

---

### 8.2 Provider: Vanilla `nginx`

**Mechanism:** For standard Nginx, `docker-release` takes full control of the `nginx.conf` or a specific `conf.d` file. This gives us the most granular control over routing logic.

#### A. The User Configuration (docker-compose.yml)

```yaml
services:
  webapp:
    image: myapp:v2
    labels:
      - "release.enable=true"
      - "release.provider=nginx"
      - "release.strategy=blue-green"
```

#### B. The Generated Artifact

**File:** `/etc/nginx/conf.d/default.conf`

**Scenario:** Blue-Green deployment. We have switched traffic to Green (New), but Blue (Old) is still alive (Soak Time) but receiving _no_ traffic.

```nginx
upstream backend_service {
    # GREEN Environment (New - Active)
    server 172.18.0.12:3000;
    server 172.18.0.13:3000;

    # BLUE Environment (Old - Idle/Backup)
    # Commented out by docker-release to ensure zero traffic
    # server 172.18.0.5:3000 backup;
    # server 172.18.0.6:3000 backup;
}

server {
    listen 80;
    location / {
        proxy_pass http://backend_service;
        proxy_set_header Host $host;
    }
}

```

---

### 8.3 Provider: `traefik`

**Mechanism:** We cannot rely on Docker labels _on the containers_ for dynamic weighting because we can't change labels on a running container. Instead, we use Traefik's **File Provider**. `docker-release` generates a dynamic YAML configuration that Traefik watches and hot-reloads.

#### A. The User Configuration (docker-compose.yml)

```yaml
services:
  webapp:
    image: myapp:v2
    labels:
      - "release.enable=true"
      - "release.provider=traefik"
      - "release.strategy=linear"
```

#### B. The Generated Artifact

**File:** `/etc/traefik/dynamic/webapp-router.yml` (Mounted in Traefik)

**Scenario:** Linear Deployment. We are currently "draining" an old container (`172.18.0.2`) and shifting traffic to a new one (`172.18.0.9`).

```yaml
http:
  services:
    webapp-service:
      loadBalancer:
        servers:
          # New Container (Healthy)
          - url: "http://172.18.0.9:80"

          # Old Container (Draining)
          # Removed by docker-release to stop new requests.
          # The container still exists in Docker (so existing requests finish),
          # but Traefik knows it is gone.
```

**Alternate Scenario: Canary with Traefik**
If this were a Canary deployment, `docker-release` would generate a Weighted Round Robin service:

```yaml
http:
  services:
    webapp-main:
      weighted:
        services:
          - name: webapp-v1 # Old
            weight: 70
          - name: webapp-v2 # New
            weight: 30

    webapp-v1:
      loadBalancer:
        servers:
          - url: "http://172.18.0.5:80"

    webapp-v2:
      loadBalancer:
        servers:
          - url: "http://172.18.0.9:80"
```
