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
	runs, err := LoadRuns(configDir)
	if err != nil {
		return err
	}
	runs[name] = rec
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		return err
	}
	data, err := toml.Marshal(runs)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(configDir, runsFile), append([]byte(runsHeader), data...), 0o644)
}
