// Package git is a tiny, no-shell helper for the one write nfu ever makes to a
// flake's version control: committing an updated flake.lock — and only
// flake.lock — with a generated message. It never stages other files and never
// pushes.
package git

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// LockFile is the single path nfu is allowed to stage and commit.
const LockFile = "flake.lock"

// run executes git in dir with an arg slice (no shell), returning trimmed
// stdout and, on failure, an error carrying trimmed stderr.
func run(ctx context.Context, dir string, args ...string) (string, error) {
	bin, err := exec.LookPath("git")
	if err != nil {
		return "", fmt.Errorf("git not found in PATH")
	}
	full := append([]string{"-C", dir}, args...)
	cmd := exec.CommandContext(ctx, bin, full...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), msg)
	}
	return strings.TrimSpace(stdout.String()), nil
}

// IsRepo reports whether dir is inside a git work tree.
func IsRepo(ctx context.Context, dir string) bool {
	out, err := run(ctx, dir, "rev-parse", "--is-inside-work-tree")
	return err == nil && out == "true"
}

// LockChanged reports whether flake.lock has uncommitted changes (modified or
// newly created) in dir's work tree.
func LockChanged(ctx context.Context, dir string) (bool, error) {
	out, err := run(ctx, dir, "status", "--porcelain", "--", LockFile)
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) != "", nil
}

// CommitLock commits ONLY flake.lock with the given message. It stages the file
// first (covering a not-yet-tracked flake.lock) and uses a pathspec commit so
// nothing else in the index is swept in. The caller must ensure there is
// something to commit (see LockChanged); an empty commit is an error.
func CommitLock(ctx context.Context, dir, message string) error {
	if _, err := run(ctx, dir, "add", "--", LockFile); err != nil {
		return err
	}
	if _, err := run(ctx, dir, "commit", "-m", message, "--", LockFile); err != nil {
		return err
	}
	return nil
}
