package state

import "testing"

func TestReleaseCommandQueue(t *testing.T) {
	m := NewManager(t.TempDir(), "demo")

	cmd, err := m.EnqueueReleaseCommand("app", true)
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if cmd.ID == "" {
		t.Fatal("expected command ID")
	}

	pending, err := m.PendingReleaseCommands()
	if err != nil {
		t.Fatalf("pending: %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("pending count = %d, want 1", len(pending))
	}
	if pending[0].Service != "app" || !pending[0].Force {
		t.Fatalf("pending command = %+v", pending[0].ReleaseCommand)
	}

	claimed, ok, err := m.ClaimReleaseCommand(pending[0])
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if !ok {
		t.Fatal("expected command claim")
	}

	pending, err = m.PendingReleaseCommands()
	if err != nil {
		t.Fatalf("pending after claim: %v", err)
	}
	if len(pending) != 0 {
		t.Fatalf("pending after claim count = %d, want 0", len(pending))
	}

	if err := m.CompleteReleaseCommand(claimed); err != nil {
		t.Fatalf("complete: %v", err)
	}
}

func TestPendingReleaseCommandsMissingDir(t *testing.T) {
	m := NewManager(t.TempDir(), "demo")

	pending, err := m.PendingReleaseCommands()
	if err != nil {
		t.Fatalf("pending: %v", err)
	}
	if len(pending) != 0 {
		t.Fatalf("pending count = %d, want 0", len(pending))
	}
}
