package config

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/malico/docker-release/internal/docker"
)

var sanitizeRe = regexp.MustCompile(`[^a-z0-9_-]+`)

func DetectProject(ctx context.Context, dockerClient *docker.Client) (string, error) {
	if project, err := fromOwnContainer(ctx, dockerClient); err == nil && project != "" {
		return project, nil
	}

	if project := fromEnv(); project != "" {
		return project, nil
	}

	if project, _ := fromComposeFile(); project != "" {
		return project, nil
	}

	if project := fromCWD(); project != "" {
		return project, nil
	}

	return "", fmt.Errorf("could not detect compose project name")
}

func fromOwnContainer(ctx context.Context, dockerClient *docker.Client) (string, error) {
	containerID, err := os.Hostname()
	if err != nil || containerID == "" {
		return "", fmt.Errorf("no hostname")
	}

	info, err := dockerClient.Inspect(ctx, containerID)
	if err != nil {
		return "", err
	}

	if info.Config == nil || info.Config.Labels == nil {
		return "", fmt.Errorf("no labels on own container")
	}

	project := info.Config.Labels["com.docker.compose.project"]
	if project == "" {
		return "", fmt.Errorf("no compose project label")
	}

	return project, nil
}

func fromEnv() string {
	return os.Getenv("COMPOSE_PROJECT_NAME")
}

func fromComposeFile() (string, error) {
	for _, name := range []string{"compose.yml", "compose.yaml", "docker-compose.yml", "docker-compose.yaml"} {
		path := name
		if wd, err := os.Getwd(); err == nil {
			path = filepath.Join(wd, name)
		}

		content, err := os.ReadFile(path)
		if err != nil {
			continue
		}

		if project := parseComposeName(string(content)); project != "" {
			return project, nil
		}
	}

	return "", fmt.Errorf("no compose file found")
}

func parseComposeName(content string) string {
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "name:") {
			value := strings.TrimSpace(strings.TrimPrefix(trimmed, "name:"))
			value = strings.Trim(value, `"'`)
			if value != "" {
				return value
			}
		}
	}

	return ""
}

func fromCWD() string {
	wd, err := os.Getwd()
	if err != nil {
		return ""
	}

	name := strings.ToLower(filepath.Base(wd))
	name = sanitizeRe.ReplaceAllString(name, "-")
	name = strings.Trim(name, "-")

	return name
}
