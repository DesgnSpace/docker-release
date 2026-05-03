package controller

import (
	"testing"

	"github.com/docker/docker/api/types"
)

func TestSplitByRevisionUsesNewestComposeConfigHash(t *testing.T) {
	t.Parallel()

	containers := []types.Container{
		containerWithRevision("old-1", 10, "image-a", "hash-old"),
		containerWithRevision("old-2", 11, "image-a", "hash-old"),
		containerWithRevision("new-1", 20, "image-a", "hash-new"),
		containerWithRevision("new-2", 21, "image-a", "hash-new"),
	}

	oldContainers, newContainers := splitByRevision(containers, groupByRevision(containers))

	assertContainerIDs(t, oldContainers, []string{"old-1", "old-2"})
	assertContainerIDs(t, newContainers, []string{"new-1", "new-2"})
}

func TestSplitByRevisionFallsBackToImageID(t *testing.T) {
	t.Parallel()

	containers := []types.Container{
		containerWithRevision("old-1", 10, "image-a", ""),
		containerWithRevision("new-1", 20, "image-b", ""),
	}

	oldContainers, newContainers := splitByRevision(containers, groupByRevision(containers))

	assertContainerIDs(t, oldContainers, []string{"old-1"})
	assertContainerIDs(t, newContainers, []string{"new-1"})
}

func containerWithRevision(id string, created int64, imageID string, configHash string) types.Container {
	labels := map[string]string{}
	if configHash != "" {
		labels["com.docker.compose.config-hash"] = configHash
	}

	return types.Container{
		ID:      id,
		Created: created,
		ImageID: imageID,
		Labels:  labels,
	}
}

func assertContainerIDs(t *testing.T, containers []types.Container, want []string) {
	t.Helper()

	if len(containers) != len(want) {
		t.Fatalf("len(containers) = %d, want %d", len(containers), len(want))
	}

	for i := range want {
		if containers[i].ID != want[i] {
			t.Fatalf("containers[%d].ID = %q, want %q", i, containers[i].ID, want[i])
		}
	}
}
