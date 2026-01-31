package healthcheck

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/malico/docker-release/internal/config"
)

type DockerClient interface {
	Stop(ctx context.Context, containerID string, timeoutSeconds int) error
	Remove(ctx context.Context, containerID string) error
	RestartCount(ctx context.Context, containerID string) (int, error)
}

type containerState struct {
	status           string
	consecutiveFails int
	startedAt        time.Time
}

type Checker struct {
	docker     DockerClient
	cfg        config.HealthCheckConfig
	httpClient *http.Client
	addrs      map[string]string

	mu     sync.RWMutex
	states map[string]*containerState
	cancel context.CancelFunc
}

func New(docker DockerClient, cfg config.HealthCheckConfig) *Checker {
	return &Checker{
		docker: docker,
		cfg:    cfg,
		httpClient: &http.Client{
			Timeout: cfg.Timeout,
		},
		addrs:  make(map[string]string),
		states: make(map[string]*containerState),
	}
}

func (c *Checker) Start(ctx context.Context, containerIDs []string, addrs map[string]string) {
	c.mu.Lock()
	now := time.Now()
	for _, id := range containerIDs {
		c.states[id] = &containerState{
			status:    "starting",
			startedAt: now,
		}
		c.addrs[id] = addrs[id]
	}
	c.mu.Unlock()

	pollCtx, cancel := context.WithCancel(ctx)
	c.cancel = cancel

	go c.poll(pollCtx)
}

func (c *Checker) Shutdown() {
	if c.cancel != nil {
		c.cancel()
	}
}

func (c *Checker) poll(ctx context.Context) {
	ticker := time.NewTicker(c.cfg.Interval)
	defer ticker.Stop()

	c.checkAll()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.checkAll()
		}
	}
}

func (c *Checker) checkAll() {
	c.mu.RLock()
	ids := make([]string, 0, len(c.states))
	for id := range c.states {
		ids = append(ids, id)
	}
	c.mu.RUnlock()

	for _, id := range ids {
		c.checkOne(id)
	}
}

func (c *Checker) checkOne(containerID string) {
	c.mu.RLock()
	addr := c.addrs[containerID]
	st := c.states[containerID]
	c.mu.RUnlock()

	if st == nil || addr == "" {
		return
	}

	healthy := c.httpCheck(addr)

	c.mu.Lock()
	defer c.mu.Unlock()

	inStartPeriod := c.cfg.StartPeriod > 0 && time.Since(st.startedAt) < c.cfg.StartPeriod

	if healthy {
		st.status = "healthy"
		st.consecutiveFails = 0
		return
	}

	if inStartPeriod {
		return
	}

	st.consecutiveFails++
	if st.consecutiveFails >= c.cfg.Retries {
		st.status = "unhealthy"
		log.Printf("[healthcheck] container %s is unhealthy after %d consecutive failures", shortID(containerID), st.consecutiveFails)
	}
}

func (c *Checker) httpCheck(addr string) bool {
	url := fmt.Sprintf("http://%s%s", addr, c.cfg.Path)

	resp, err := c.httpClient.Get(url)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	return resp.StatusCode >= 200 && resp.StatusCode < 400
}

func (c *Checker) WaitHealthy(ctx context.Context, containerID string, timeout time.Duration) error {
	deadline := time.After(timeout)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-deadline:
			return fmt.Errorf("container %s did not become healthy within %s", shortID(containerID), timeout)
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			c.mu.RLock()
			st := c.states[containerID]
			c.mu.RUnlock()

			if st == nil {
				return nil
			}

			if st.status == "healthy" {
				return nil
			}

			if st.status == "unhealthy" {
				return fmt.Errorf("container %s is unhealthy", shortID(containerID))
			}
		}
	}
}

func (c *Checker) Stop(ctx context.Context, containerID string, timeoutSeconds int) error {
	return c.docker.Stop(ctx, containerID, timeoutSeconds)
}

func (c *Checker) Remove(ctx context.Context, containerID string) error {
	return c.docker.Remove(ctx, containerID)
}

func (c *Checker) IsHealthy(_ context.Context, containerID string) (bool, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	st := c.states[containerID]
	if st == nil {
		return true, nil
	}

	return st.status != "unhealthy", nil
}

func (c *Checker) RestartCount(ctx context.Context, containerID string) (int, error) {
	return c.docker.RestartCount(ctx, containerID)
}

func shortID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}
