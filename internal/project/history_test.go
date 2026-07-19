package project

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRunsRoundTrip(t *testing.T) {
	dir := t.TempDir()

	runs, err := LoadRuns(dir)
	if err != nil {
		t.Fatalf("LoadRuns on empty dir: %v", err)
	}
	if len(runs) != 0 {
		t.Fatalf("expected empty history, got %v", runs)
	}

	at := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	if err := RecordRun(dir, "nfu", RunRecord{At: at, OK: true, Elapsed: 83, Detail: "updated 2 · switched"}); err != nil {
		t.Fatalf("RecordRun: %v", err)
	}
	if err := RecordRun(dir, "lab", RunRecord{At: at, OK: false, Detail: "deploy failed"}); err != nil {
		t.Fatalf("RecordRun second: %v", err)
	}

	runs, err = LoadRuns(dir)
	if err != nil {
		t.Fatalf("LoadRuns: %v", err)
	}
	if len(runs) != 2 {
		t.Fatalf("expected 2 records, got %d", len(runs))
	}
	got := runs["nfu"]
	if !got.OK || got.Elapsed != 83 || !got.At.Equal(at) || got.Detail != "updated 2 · switched" {
		t.Fatalf("bad record: %+v", got)
	}
	if runs["lab"].OK {
		t.Fatalf("lab should be failed")
	}
}

func TestLoadRunsCorruptStartsFresh(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, runsFile), []byte("not toml ["), 0o644); err != nil {
		t.Fatal(err)
	}
	runs, err := LoadRuns(dir)
	if err != nil {
		t.Fatalf("corrupt history should not error: %v", err)
	}
	if len(runs) != 0 {
		t.Fatalf("expected fresh history, got %v", runs)
	}
}
