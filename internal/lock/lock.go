// Package lock orchestrates the one write nfu makes after moving inputs:
// committing flake.lock. It sits above internal/git (the raw commands) and is
// shared by the CLI and the TUI so both commit identically.
package lock

import (
	"context"
	"fmt"
	"strings"

	"github.com/conner-s/nfu/internal/flake"
	"github.com/conner-s/nfu/internal/git"
	"github.com/conner-s/nfu/internal/ui"
)

// AnyMoved reports whether any input's pin actually changed.
func AnyMoved(results []flake.UpdateResult) bool {
	for _, r := range results {
		if r.Moved() {
			return true
		}
	}
	return false
}

// Message builds the flake.lock commit subject from the inputs that moved.
func Message(results []flake.UpdateResult) string {
	var moved []flake.UpdateResult
	for _, r := range results {
		if r.Moved() {
			moved = append(moved, r)
		}
	}
	switch len(moved) {
	case 0:
		return ""
	case 1:
		r := moved[0]
		return fmt.Sprintf("flake.lock: update %s (%s → %s)", r.Input, ui.ShortPin(r.OldPin), ui.ShortPin(r.NewPin))
	default:
		names := make([]string, len(moved))
		for i, r := range moved {
			names[i] = r.Input
		}
		return "flake.lock: update " + strings.Join(names, ", ")
	}
}

// Commit commits flake.lock when enabled and something actually moved. A missing
// repo or a commit failure is non-fatal — the update already succeeded — and is
// reported via warn rather than an error, so callers surface it without failing.
func Commit(ctx context.Context, root string, results []flake.UpdateResult, enabled bool) (committed bool, msg, warn string) {
	if !enabled || !AnyMoved(results) {
		return false, "", ""
	}
	if !git.IsRepo(ctx, root) {
		return false, "", "flake root is not a git repo; skipped commit"
	}
	changed, err := git.LockChanged(ctx, root)
	if err != nil {
		return false, "", "commit skipped: " + err.Error()
	}
	if !changed {
		return false, "", ""
	}
	msg = Message(results)
	if err := git.CommitLock(ctx, root, msg); err != nil {
		return false, "", "commit failed: " + err.Error()
	}
	return true, msg, ""
}
