package docker

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
)

type Client struct {
	api client.APIClient
}

func NewClient() (*Client, error) {
	api, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("connecting to docker: %w", err)
	}

	return &Client{api: api}, nil
}

func (c *Client) Ping(ctx context.Context) error {
	_, err := c.api.Ping(ctx)
	return err
}

func (c *Client) ListManagedContainers(ctx context.Context) ([]types.Container, error) {
	f := filters.NewArgs()
	f.Add("label", "release.enable=true")

	return c.api.ContainerList(ctx, container.ListOptions{Filters: f})
}

func (c *Client) Inspect(ctx context.Context, containerID string) (types.ContainerJSON, error) {
	return c.api.ContainerInspect(ctx, containerID)
}

func (c *Client) Stop(ctx context.Context, containerID string, timeoutSeconds int) error {
	timeout := timeoutSeconds
	return c.api.ContainerStop(ctx, containerID, container.StopOptions{Timeout: &timeout})
}

func (c *Client) Remove(ctx context.Context, containerID string) error {
	return c.api.ContainerRemove(ctx, containerID, container.RemoveOptions{Force: true})
}

func (c *Client) Logs(ctx context.Context, containerID string, lines string) (io.ReadCloser, error) {
	return c.api.ContainerLogs(ctx, containerID, container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Tail:       lines,
	})
}

func (c *Client) Events(ctx context.Context) (<-chan events.Message, <-chan error) {
	f := filters.NewArgs()
	f.Add("type", "container")
	f.Add("label", "release.enable=true")

	return c.api.Events(ctx, events.ListOptions{Filters: f})
}

func (c *Client) Exec(ctx context.Context, containerID string, cmd []string) error {
	exec, err := c.api.ContainerExecCreate(ctx, containerID, container.ExecOptions{
		Cmd: cmd,
	})
	if err != nil {
		return fmt.Errorf("exec create: %w", err)
	}

	return c.api.ContainerExecStart(ctx, exec.ID, container.ExecStartOptions{})
}

func (c *Client) ContainerAddr(ctx context.Context, containerID string) (string, error) {
	info, err := c.api.ContainerInspect(ctx, containerID)
	if err != nil {
		return "", fmt.Errorf("inspecting container: %w", err)
	}

	for _, network := range info.NetworkSettings.Networks {
		if network.IPAddress != "" {
			port := firstExposedPort(info)
			return fmt.Sprintf("%s:%s", network.IPAddress, port), nil
		}
	}

	return "", fmt.Errorf("no network address for container %s", containerID[:12])
}

func firstExposedPort(info types.ContainerJSON) string {
	for port := range info.Config.ExposedPorts {
		return port.Port()
	}
	return "80"
}

func (c *Client) ResolveAddr(ctx context.Context, containerID string) (string, error) {
	return c.ContainerAddr(ctx, containerID)
}

func (c *Client) IsHealthy(ctx context.Context, containerID string) (bool, error) {
	info, err := c.api.ContainerInspect(ctx, containerID)
	if err != nil {
		return false, fmt.Errorf("inspecting container: %w", err)
	}

	if !info.State.Running {
		return false, nil
	}

	if info.State.Health == nil {
		return true, nil
	}

	return info.State.Health.Status == "healthy", nil
}

func (c *Client) RestartCount(ctx context.Context, containerID string) (int, error) {
	info, err := c.api.ContainerInspect(ctx, containerID)
	if err != nil {
		return 0, fmt.Errorf("inspecting container: %w", err)
	}

	return info.RestartCount, nil
}

func (c *Client) WaitHealthy(ctx context.Context, containerID string, timeout time.Duration) error {
	deadline := time.After(timeout)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-deadline:
			return fmt.Errorf("container %s did not become healthy within %s", containerID[:12], timeout)
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			info, err := c.api.ContainerInspect(ctx, containerID)
			if err != nil {
				return fmt.Errorf("inspecting container: %w", err)
			}

			if info.State.Health == nil {
				return nil
			}

			if info.State.Health.Status == "healthy" {
				return nil
			}
		}
	}
}

func (c *Client) Close() error {
	return c.api.Close()
}
