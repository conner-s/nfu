package flake

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// Upstream resolves what the named inputs would lock to right now — the current
// upstream state — WITHOUT touching the real flake.lock. It runs `nix flake
// update <names…> --output-lock-file <tmp>`, which re-locks against the existing
// flake.lock as the reference base and writes the result to a throwaway file,
// then parses that file into the same []Input the picker already renders. With no
// names it re-locks every input (nix's whole-flake update).
//
// This is the read-only "what would an update give me" probe behind the picker's
// upstream-check hotkeys. Because it is a real lock, moved inputs are FETCHED to
// compute their narHash — a moved nixpkgs is a full source fetch, so this is not
// cheap — but that fetch warms nix's cache for the subsequent real update.
//
// The returned slice covers every input in the probe lock (the whole graph is
// written regardless of which inputs were asked for); the caller picks out the
// names it requested. Names must be real, non-`follows` inputs: nix errors on a
// follows alias, so the picker guards those before calling.
func (c *Client) Upstream(ctx context.Context, names ...string) ([]Input, error) {
	dir, err := os.MkdirTemp("", "nfu-upstream-")
	if err != nil {
		return nil, fmt.Errorf("upstream probe: %w", err)
	}
	defer os.RemoveAll(dir)
	out := filepath.Join(dir, "flake.lock")

	args := nixArgs("flake", "update")
	args = append(args, names...)
	args = append(args, "--output-lock-file", out)
	if _, err := c.run(ctx, "nix", args...); err != nil {
		return nil, err
	}

	data, err := os.ReadFile(out)
	if err != nil {
		return nil, fmt.Errorf("read upstream lock: %w", err)
	}
	md, err := parseLockFile(data)
	if err != nil {
		return nil, err
	}
	return md.buildInputs()
}
