package project

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/conner-s/nfu/internal/git"
)

// Pending is one repository's worth of uncommitted project files — the offer
// `nfu project add` / `nfu project edit` makes after writing a definition.
// Projects are meant to be version-controlled (that is what a configurable
// projectsDir is for), so when they live in a repo the write is only half the
// job.
type Pending struct {
	Root    string   // the work tree holding the files
	Paths   []string // repo-relative paths with uncommitted changes
	Message string   // proposed commit subject
}

// PendingFiles groups paths by the git work tree that holds them, keeping only
// those with something to commit. A project file and the index can sit in
// different repos (projectsDir is configurable), hence the grouping; the common
// case returns one entry, or none when nothing is tracked. This is an offer, not
// an operation: anything git cannot answer for is silently left out rather than
// failing a save that already succeeded.
func PendingFiles(ctx context.Context, message string, paths ...string) []Pending {
	byRoot := map[string][]string{}
	var roots []string
	for _, p := range paths {
		root, rel, ok := locate(ctx, p)
		if !ok {
			continue
		}
		if changed, err := git.Changed(ctx, root, rel); err != nil || !changed {
			continue
		}
		if _, seen := byRoot[root]; !seen {
			roots = append(roots, root)
		}
		if !containsPath(byRoot[root], rel) {
			byRoot[root] = append(byRoot[root], rel)
		}
	}
	sort.Strings(roots)
	out := make([]Pending, 0, len(roots))
	for _, r := range roots {
		sort.Strings(byRoot[r])
		out = append(out, Pending{Root: r, Paths: byRoot[r], Message: message})
	}
	return out
}

// Commit commits the pending files with the proposed message.
func (p Pending) Commit(ctx context.Context) error {
	return git.Commit(ctx, p.Root, p.Message, p.Paths...)
}

// Label summarizes the change set for a prompt: "totality.toml → nixos-config".
func (p Pending) Label() string {
	return strings.Join(p.Paths, ", ") + " → " + filepath.Base(p.Root)
}

// CommitMessage is the generated subject for a project definition change,
// mirroring lock.Message's "what moved" style for flake.lock.
func CommitMessage(verb, name string) string {
	return fmt.Sprintf("nfu: %s project %s", verb, name)
}

// locate resolves path to (work tree root, path relative to it). Symlinks are
// resolved first: a ~/.config/nfu symlinked into a dotfiles repo would otherwise
// produce a relative path escaping the root git reports.
func locate(ctx context.Context, path string) (root, rel string, ok bool) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", "", false
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		abs = resolved
	}
	root, err = git.Root(ctx, filepath.Dir(abs))
	if err != nil || root == "" {
		return "", "", false
	}
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	rel, err = filepath.Rel(root, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", "", false
	}
	return root, rel, true
}

func containsPath(ss []string, v string) bool {
	for _, s := range ss {
		if s == v {
			return true
		}
	}
	return false
}
