package state

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMissing(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager(dir)

	s, err := mgr.Load("webapp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if s.Service != "webapp" {
		t.Errorf("service = %s, want webapp", s.Service)
	}
	if s.Status != StatusIdle {
		t.Errorf("status = %s, want idle", s.Status)
	}
}

func TestSaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager(dir)

	original := &DeploymentState{
		Service:              "webapp",
		Status:               StatusInProgress,
		Strategy:             "canary",
		CurrentWeight:        25,
		ActiveDeploymentID:   "deploy_abc",
		PreviousDeploymentID: "deploy_xyz",
		Containers: Containers{
			Stable: []string{"c1", "c2"},
			Canary: []string{"c3"},
		},
	}

	if err := mgr.Save(original); err != nil {
		t.Fatalf("save error: %v", err)
	}

	loaded, err := mgr.Load("webapp")
	if err != nil {
		t.Fatalf("load error: %v", err)
	}

	if loaded.Status != StatusInProgress {
		t.Errorf("status = %s, want in_progress", loaded.Status)
	}
	if loaded.CurrentWeight != 25 {
		t.Errorf("weight = %d, want 25", loaded.CurrentWeight)
	}
	if loaded.ActiveDeploymentID != "deploy_abc" {
		t.Errorf("active = %s, want deploy_abc", loaded.ActiveDeploymentID)
	}
	if len(loaded.Containers.Stable) != 2 {
		t.Errorf("stable count = %d, want 2", len(loaded.Containers.Stable))
	}
	if len(loaded.Containers.Canary) != 1 {
		t.Errorf("canary count = %d, want 1", len(loaded.Containers.Canary))
	}
}

func TestSaveAtomic(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager(dir)

	s := &DeploymentState{Service: "webapp", Status: StatusIdle}
	if err := mgr.Save(s); err != nil {
		t.Fatalf("save error: %v", err)
	}

	// No .tmp file should remain
	tmp := filepath.Join(dir, "webapp_state.json.tmp")
	if _, err := os.Stat(tmp); !os.IsNotExist(err) {
		t.Error("temp file was not cleaned up")
	}

	// Actual file should exist
	actual := filepath.Join(dir, "webapp_state.json")
	if _, err := os.Stat(actual); err != nil {
		t.Errorf("state file missing: %v", err)
	}
}

func TestGenerateDeploymentID(t *testing.T) {
	id1 := GenerateDeploymentID()
	id2 := GenerateDeploymentID()

	if id1 == "" {
		t.Error("deployment ID should not be empty")
	}

	if !testing.Short() && id1 == id2 {
		t.Error("deployment IDs should be unique")
	}

	if len(id1) < 20 {
		t.Errorf("deployment ID too short: %s", id1)
	}
}

func TestSaveCreatesDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "state")
	mgr := NewManager(dir)

	s := &DeploymentState{Service: "webapp", Status: StatusIdle}
	if err := mgr.Save(s); err != nil {
		t.Fatalf("save error: %v", err)
	}

	if _, err := os.Stat(dir); err != nil {
		t.Errorf("dir not created: %v", err)
	}
}
