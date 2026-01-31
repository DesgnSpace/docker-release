package monitor

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"
)

var ErrUnhealthy = errors.New("unhealthy container detected")

type HealthChecker interface {
	IsHealthy(ctx context.Context, containerID string) (bool, error)
	RestartCount(ctx context.Context, containerID string) (int, error)
}

type HealthMonitor struct {
	checker          HealthChecker
	interval         time.Duration
	gracePeriod      time.Duration
	maxRestarts      int
	containerIDs     []string
	initialRestarts  map[string]int
	onUnhealthy      func(containerID string, reason string)
}

func NewHealthMonitor(checker HealthChecker, containerIDs []string, onUnhealthy func(string, string)) *HealthMonitor {
	return &HealthMonitor{
		checker:         checker,
		interval:        5 * time.Second,
		maxRestarts:     3,
		containerIDs:    containerIDs,
		initialRestarts: make(map[string]int),
		onUnhealthy:     onUnhealthy,
	}
}

func (m *HealthMonitor) SetInterval(d time.Duration) {
	m.interval = d
}

func (m *HealthMonitor) SetMaxRestarts(n int) {
	m.maxRestarts = n
}

func (m *HealthMonitor) SetGracePeriod(d time.Duration) {
	m.gracePeriod = d
}

func (m *HealthMonitor) Run(ctx context.Context) error {
	if m.gracePeriod > 0 {
		log.Printf("[monitor] waiting %s grace period before health checks", m.gracePeriod)
		select {
		case <-time.After(m.gracePeriod):
		case <-ctx.Done():
			return nil
		}
	}

	for _, id := range m.containerIDs {
		count, err := m.checker.RestartCount(ctx, id)
		if err != nil {
			return fmt.Errorf("getting initial restart count for %s: %w", id[:12], err)
		}
		m.initialRestarts[id] = count
	}

	log.Printf("[monitor] watching %d container(s), interval=%s, max_restarts=%d", len(m.containerIDs), m.interval, m.maxRestarts)

	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			triggered, err := m.check(ctx)
			if err != nil {
				return err
			}
			if triggered {
				return ErrUnhealthy
			}
		}
	}
}

func (m *HealthMonitor) check(ctx context.Context) (bool, error) {
	for _, id := range m.containerIDs {
		healthy, err := m.checker.IsHealthy(ctx, id)
		if err != nil {
			log.Printf("[monitor] warning: checking %s: %v", id[:12], err)
			continue
		}

		if !healthy {
			reason := fmt.Sprintf("container %s is unhealthy", id[:12])
			log.Printf("[monitor] %s", reason)
			m.onUnhealthy(id, reason)
			return true, nil
		}

		count, err := m.checker.RestartCount(ctx, id)
		if err != nil {
			log.Printf("[monitor] warning: restart count %s: %v", id[:12], err)
			continue
		}

		restarts := count - m.initialRestarts[id]
		if restarts >= m.maxRestarts {
			reason := fmt.Sprintf("container %s restarted %d times (max %d)", id[:12], restarts, m.maxRestarts)
			log.Printf("[monitor] %s", reason)
			m.onUnhealthy(id, reason)
			return true, nil
		}
	}

	return false, nil
}
