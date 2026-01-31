package healthcheck

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/malico/docker-release/internal/config"
)

type mockDockerClient struct {
	mu            sync.Mutex
	stopCalls     []string
	removeCalls   []string
	restartCounts map[string]int
}

func (m *mockDockerClient) Stop(_ context.Context, containerID string, _ int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stopCalls = append(m.stopCalls, containerID)
	return nil
}

func (m *mockDockerClient) Remove(_ context.Context, containerID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.removeCalls = append(m.removeCalls, containerID)
	return nil
}

func (m *mockDockerClient) RestartCount(_ context.Context, containerID string) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.restartCounts[containerID], nil
}

func TestCheckerBecomesHealthy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	addr := strings.TrimPrefix(srv.URL, "http://")

	docker := &mockDockerClient{restartCounts: make(map[string]int)}
	cfg := config.HealthCheckConfig{
		Path:     "/health",
		Interval: 20 * time.Millisecond,
		Timeout:  time.Second,
		Retries:  3,
	}

	checker := New(docker, cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	containerID := "abc123def456_full_id"
	checker.Start(ctx, []string{containerID}, map[string]string{containerID: addr})
	defer checker.Shutdown()

	err := checker.WaitHealthy(ctx, containerID, time.Second)
	if err != nil {
		t.Fatalf("expected container to become healthy: %v", err)
	}

	healthy, err := checker.IsHealthy(ctx, containerID)
	if err != nil {
		t.Fatalf("IsHealthy error: %v", err)
	}
	if !healthy {
		t.Error("expected IsHealthy to return true")
	}
}

func TestCheckerBecomesUnhealthy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	addr := strings.TrimPrefix(srv.URL, "http://")

	docker := &mockDockerClient{restartCounts: make(map[string]int)}
	cfg := config.HealthCheckConfig{
		Path:     "/health",
		Interval: 20 * time.Millisecond,
		Timeout:  time.Second,
		Retries:  3,
	}

	checker := New(docker, cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	containerID := "abc123def456_full_id"
	checker.Start(ctx, []string{containerID}, map[string]string{containerID: addr})
	defer checker.Shutdown()

	err := checker.WaitHealthy(ctx, containerID, time.Second)
	if err == nil {
		t.Fatal("expected error for unhealthy container")
	}

	if !strings.Contains(err.Error(), "unhealthy") {
		t.Errorf("expected unhealthy error, got: %v", err)
	}
}

func TestCheckerConsecutiveFailureCounting(t *testing.T) {
	var mu sync.Mutex
	requestCount := 0

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requestCount++
		count := requestCount
		mu.Unlock()

		if count <= 2 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	addr := strings.TrimPrefix(srv.URL, "http://")

	docker := &mockDockerClient{restartCounts: make(map[string]int)}
	cfg := config.HealthCheckConfig{
		Path:     "/health",
		Interval: 20 * time.Millisecond,
		Timeout:  time.Second,
		Retries:  5,
	}

	checker := New(docker, cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	containerID := "abc123def456_full_id"
	checker.Start(ctx, []string{containerID}, map[string]string{containerID: addr})
	defer checker.Shutdown()

	err := checker.WaitHealthy(ctx, containerID, time.Second)
	if err != nil {
		t.Fatalf("expected container to become healthy after initial failures: %v", err)
	}
}

func TestCheckerStartPeriodGrace(t *testing.T) {
	var mu sync.Mutex
	shouldSucceed := false

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		ok := shouldSucceed
		mu.Unlock()

		if !ok {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	addr := strings.TrimPrefix(srv.URL, "http://")

	docker := &mockDockerClient{restartCounts: make(map[string]int)}
	cfg := config.HealthCheckConfig{
		Path:        "/health",
		Interval:    20 * time.Millisecond,
		Timeout:     time.Second,
		Retries:     2,
		StartPeriod: 200 * time.Millisecond,
	}

	checker := New(docker, cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	containerID := "abc123def456_full_id"
	checker.Start(ctx, []string{containerID}, map[string]string{containerID: addr})
	defer checker.Shutdown()

	time.Sleep(100 * time.Millisecond)

	checker.mu.RLock()
	st := checker.states[containerID]
	status := st.status
	checker.mu.RUnlock()

	if status == "unhealthy" {
		t.Error("container should not be unhealthy during start period")
	}

	mu.Lock()
	shouldSucceed = true
	mu.Unlock()

	err := checker.WaitHealthy(ctx, containerID, 2*time.Second)
	if err != nil {
		t.Fatalf("expected container to become healthy after start period: %v", err)
	}
}

func TestCheckerDelegatesDockerOps(t *testing.T) {
	docker := &mockDockerClient{
		restartCounts: map[string]int{"container_abc123": 5},
	}
	cfg := config.HealthCheckConfig{
		Path:     "/health",
		Interval: time.Second,
		Timeout:  time.Second,
		Retries:  3,
	}

	checker := New(docker, cfg)
	ctx := context.Background()

	if err := checker.Stop(ctx, "container_abc123", 10); err != nil {
		t.Fatalf("Stop error: %v", err)
	}

	if err := checker.Remove(ctx, "container_abc123"); err != nil {
		t.Fatalf("Remove error: %v", err)
	}

	count, err := checker.RestartCount(ctx, "container_abc123")
	if err != nil {
		t.Fatalf("RestartCount error: %v", err)
	}

	docker.mu.Lock()
	defer docker.mu.Unlock()

	if len(docker.stopCalls) != 1 || docker.stopCalls[0] != "container_abc123" {
		t.Errorf("expected Stop delegation, got %v", docker.stopCalls)
	}

	if len(docker.removeCalls) != 1 || docker.removeCalls[0] != "container_abc123" {
		t.Errorf("expected Remove delegation, got %v", docker.removeCalls)
	}

	if count != 5 {
		t.Errorf("expected restart count 5, got %d", count)
	}
}

func TestCheckerUnknownContainerIsHealthy(t *testing.T) {
	docker := &mockDockerClient{restartCounts: make(map[string]int)}
	cfg := config.HealthCheckConfig{
		Path:     "/health",
		Interval: time.Second,
		Timeout:  time.Second,
		Retries:  3,
	}

	checker := New(docker, cfg)

	healthy, err := checker.IsHealthy(context.Background(), "unknown_container")
	if err != nil {
		t.Fatalf("IsHealthy error: %v", err)
	}

	if !healthy {
		t.Error("unknown container should be considered healthy")
	}
}
