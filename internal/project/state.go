// Package project turns nfu's flake-input updater into a scoped
// update→switch→deploy pipeline. A project bundles one or more inputs to update,
// whether to rebuild the local host, and an ordered set of deploy hosts; the
// "current" project (kubectl-style) is remembered between runs so a bare
// `nfu project run` re-runs it.
//
// It is layered like the rest of nfu: pure definition/merge logic (state, store,
// catalog, plan) is unit-tested here, and run.go is the thin orchestration over
// internal/flake, internal/lock, and internal/proc.
package project

import (
	"os"
	"path/filepath"
	"strings"
)

// currentFile is the mutable marker naming the selected project, kept beside the
// config (never IN config.toml, which is read-only when home-manager generates
// it).
const currentFile = "current"

// ReadCurrent returns the selected project name, or "" if none is set. A missing
// marker is not an error.
func ReadCurrent(configDir string) (string, error) {
	data, err := os.ReadFile(filepath.Join(configDir, currentFile))
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

// WriteCurrent records name as the selected project, creating configDir if
// needed. An empty name clears the selection.
func WriteCurrent(configDir, name string) error {
	name = strings.TrimSpace(name)
	path := filepath.Join(configDir, currentFile)
	if name == "" {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(name+"\n"), 0o644)
}
