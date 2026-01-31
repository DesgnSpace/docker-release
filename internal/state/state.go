package state

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type Status string

const (
	StatusIdle        Status = "idle"
	StatusInProgress  Status = "in_progress"
	StatusRollingBack Status = "rolling_back"
)

const DefaultStaleThreshold = 30 * time.Minute

type DeploymentState struct {
	Service              string     `json:"service"`
	Status               Status     `json:"status"`
	Strategy             string     `json:"strategy"`
	CurrentWeight        int        `json:"current_weight"`
	ActiveDeploymentID   string     `json:"active_deployment_id"`
	PreviousDeploymentID string     `json:"previous_deployment_id"`
	Containers           Containers `json:"containers"`
	UpdatedAt            time.Time  `json:"updated_at"`
}

func (s *DeploymentState) IsStale(threshold time.Duration) bool {
	if s.Status != StatusInProgress && s.Status != StatusRollingBack {
		return false
	}

	if s.UpdatedAt.IsZero() {
		return true
	}

	return time.Since(s.UpdatedAt) > threshold
}

type Containers struct {
	Stable []string `json:"stable"`
	Canary []string `json:"canary"`
}

type Manager struct {
	dir string
}

func NewManager(dir string) *Manager {
	return &Manager{dir: dir}
}

func (m *Manager) Load(service string) (*DeploymentState, error) {
	path := m.path(service)

	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &DeploymentState{
			Service: service,
			Status:  StatusIdle,
		}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading state file: %w", err)
	}

	var s DeploymentState
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("parsing state file: %w", err)
	}

	return &s, nil
}

func (m *Manager) Save(s *DeploymentState) error {
	s.UpdatedAt = time.Now()

	if err := os.MkdirAll(m.dir, 0o755); err != nil {
		return fmt.Errorf("creating state dir: %w", err)
	}

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling state: %w", err)
	}

	tmp := m.path(s.Service) + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("writing temp state file: %w", err)
	}

	if err := os.Rename(tmp, m.path(s.Service)); err != nil {
		return fmt.Errorf("renaming state file: %w", err)
	}

	return nil
}

func GenerateDeploymentID() string {
	b := make([]byte, 4)
	rand.Read(b)
	return fmt.Sprintf("deploy_%s_%s", time.Now().Format("20060102150405"), hex.EncodeToString(b))
}

func (m *Manager) path(service string) string {
	return filepath.Join(m.dir, service+"_state.json")
}
