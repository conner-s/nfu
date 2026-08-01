// Package git is a tiny, no-shell helper for the writes nfu makes to version
// control: committing an updated flake.lock in the flake repo, and committing a
// project definition after `nfu project add` / `nfu project edit` when the
// config directory is itself tracked. Every commit is pathspec-scoped to the
// files it was given, so nothing else in the index is ever swept in, and it
// never pushes.
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

// Root returns the top level of the work tree containing dir. It errors when dir
// is not in a repository, which is how callers tell "not tracked" from "tracked
// and clean".
func Root(ctx context.Context, dir string) (string, error) {
	return run(ctx, dir, "rev-parse", "--show-toplevel")
}

// Changed reports whether any of paths has uncommitted changes (modified or
// not yet tracked) in dir's work tree.
func Changed(ctx context.Context, dir string, paths ...string) (bool, error) {
	if len(paths) == 0 {
		return false, nil
	}
	out, err := run(ctx, dir, append([]string{"status", "--porcelain", "--"}, paths...)...)
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) != "", nil
}

// Commit stages paths and commits ONLY them with the given message. Staging
// first covers files git does not track yet, and the pathspec commit keeps
// anything else already in the index out of it. The caller must ensure there is
// something to commit (see Changed); an empty commit is an error.
func Commit(ctx context.Context, dir, message string, paths ...string) error {
	if len(paths) == 0 {
		return fmt.Errorf("git commit: no paths given")
	}
	if _, err := run(ctx, dir, append([]string{"add", "--"}, paths...)...); err != nil {
		return err
	}
	args := append([]string{"commit", "-m", message, "--"}, paths...)
	if _, err := run(ctx, dir, args...); err != nil {
		return err
	}
	return nil
}

// LockChanged reports whether flake.lock has uncommitted changes (modified or
// newly created) in dir's work tree.
func LockChanged(ctx context.Context, dir string) (bool, error) {
	return Changed(ctx, dir, LockFile)
}

// CommitLock commits ONLY flake.lock with the given message.
func CommitLock(ctx context.Context, dir, message string) error {
	return Commit(ctx, dir, message, LockFile)
}
