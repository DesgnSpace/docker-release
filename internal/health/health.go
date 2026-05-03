package health

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var (
	sentinelPath = "/var/lib/docker-release/.ready"
	manifestPath = "/var/lib/docker-release/.files"
)

// RecordFile notes that path was successfully written by a provider.
// Best-effort: errors are silently ignored so providers never fail because of this.
func RecordFile(path string) {
	if err := os.MkdirAll(filepath.Dir(manifestPath), 0o755); err != nil {
		return
	}
	f, err := os.OpenFile(manifestPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintln(f, path)
}

func MarkReady() error {
	if err := os.MkdirAll(filepath.Dir(sentinelPath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(sentinelPath, nil, 0o644)
}

func ClearReady() error {
	for _, p := range []string{sentinelPath, manifestPath} {
		if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

// IsReady returns true when the initial scan has completed and every config
// file recorded by RecordFile still exists on disk.
func IsReady() bool {
	if _, err := os.Stat(sentinelPath); err != nil {
		return false
	}

	data, err := os.ReadFile(manifestPath)
	if errors.Is(err, os.ErrNotExist) || len(strings.TrimSpace(string(data))) == 0 {
		return true
	}
	if err != nil {
		return false
	}

	for _, path := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if path == "" {
			continue
		}
		if _, err := os.Stat(path); err != nil {
			return false
		}
	}
	return true
}
