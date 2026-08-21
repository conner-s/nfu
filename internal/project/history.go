package project

import (
	"bytes"
	"os"
	"path/filepath"
	"time"

	toml "github.com/pelletier/go-toml/v2"
)

// runsFile is the mutable per-project last-run record, kept beside the config
// like the current-project marker. It exists so the manager TUI (and `list`)
// can show "last run · status" without re-deriving anything.
const runsFile = "runs.toml"

const runsHeader = "# nfu run history — last run per project. Managed by `nfu project run`.\n\n"

// RunRecord is the remembered outcome of a project's most recent run.
type RunRecord struct {
	At      time.Time `toml:"at"`
	OK      bool      `toml:"ok"`
	Elapsed int64     `toml:"elapsed,omitempty"` // seconds
	Detail  string    `toml:"detail,omitempty"`  // "updated 2 · switched · deployed x"
}

// LoadRuns reads the last-run records. A missing file is an empty history, not
// an error.
func LoadRuns(configDir string) (map[string]RunRecord, error) {
	data, err := os.ReadFile(filepath.Join(configDir, runsFile))
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]RunRecord{}, nil
		}
		return nil, err
	}
	out := map[string]RunRecord{}
	if err := toml.NewDecoder(bytes.NewReader(data)).Decode(&out); err != nil {
		// A corrupt history file should never block a run: start fresh.
		return map[string]RunRecord{}, nil
	}
	return out, nil
}

// RecordRun merges one project's record into the history, creating configDir as
// needed. Callers treat failure as best-effort (history never gates a run).
func RecordRun(configDir, name string, rec RunRecord) error {
	return record(configDir, rec, name)
}

// RecordPlan remembers a finished run against every project it actually covered.
// A plan that folded several together — a bundle's `includes`, or a --merge run —
// marks each member, so "last run" stays true for the projects a user recognizes.
// Keying it under the synthetic "a+b+c" name instead would grow runs.toml by a
// row per combination ever run, and leave the members looking untouched.
func RecordPlan(configDir string, plan Plan, rec RunRecord) error {
	if len(plan.Members) > 0 {
		return record(configDir, rec, plan.Members...)
	}
	return record(configDir, rec, plan.Name)
}

// record writes one outcome under each of names in a single read-modify-write.
func record(configDir string, rec RunRecord, names ...string) error {
	runs, err := LoadRuns(configDir)
	if err != nil {
		return err
	}
	for _, n := range names {
		runs[n] = rec
	}
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		return err
	}
	data, err := toml.Marshal(runs)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(configDir, runsFile), append([]byte(runsHeader), data...), 0o644)
}
