package flake

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// Upstream resolves what one input would lock to right now — its current
// upstream state — WITHOUT touching the real flake.lock. It runs `nix flake
// update <name> --output-lock-file <tmp>`, which re-locks against the existing
// flake.lock as the reference base and writes the result to a throwaway file,
// then parses that file back into the same Input the picker already renders.
//
// This is the read-only "what would an update give me" probe behind the picker's
// upstream-check hotkeys. Because it is a real lock, a moved input is FETCHED to
// compute its narHash — a moved nixpkgs is a full source fetch, so this is not
// cheap — but that fetch warms nix's cache for the subsequent real update.
//
// It deliberately probes ONE input per invocation. nix locks inputs strictly
// sequentially inside a single call and each one costs a live upstream round
// trip, so asking for N names at once costs the sum of N round trips and can
// report nothing until the last has landed. One name per call lets the caller
// fan out concurrently: every input answers on its own, results render as they
// arrive, and one unreachable remote costs only its own row rather than failing
// the whole sweep. (Measured against a 22-input flake: ~4.5s batched versus
// ~0.8s across 8 concurrent probes, for identical revs.)
//
// name must be a real, non-`follows` input: nix errors on a follows alias, so
// the picker guards those before calling.
//
// refresh adds nix's `--refresh`, which re-resolves the ref ignoring any cached
// answer. An ordinary probe already contacts the remote, so this is not how a
// probe becomes truthful — it is the escape hatch for when a cached tree is
// genuinely wrong, most of all a force-pushed ref still naming content nix has
// already hashed.
func (c *Client) Upstream(ctx context.Context, name string, refresh bool) (Input, error) {
	dir, err := os.MkdirTemp("", "nfu-upstream-")
	if err != nil {
		return Input{}, fmt.Errorf("upstream probe: %w", err)
	}
	defer os.RemoveAll(dir)
	out := filepath.Join(dir, "flake.lock")

	// --refresh is a nix-level flag, so it precedes the `flake update` subcommand.
	// The temp dir is per-call, so concurrent probes never share an output path.
	args := nixArgs()
	if refresh {
		args = append(args, "--refresh")
	}
	args = append(args, "flake", "update", name, "--output-lock-file", out)
	if _, err := c.run(ctx, "nix", args...); err != nil {
		return Input{}, err
	}

	data, err := os.ReadFile(out)
	if err != nil {
		return Input{}, fmt.Errorf("read upstream lock: %w", err)
	}
	md, err := parseLockFile(data)
	if err != nil {
		return Input{}, err
	}
	// The probe lock holds the WHOLE graph, not just the requested input.
	inputs, err := md.buildInputs()
	if err != nil {
		return Input{}, err
	}
	for _, in := range inputs {
		if in.Name == name {
			return in, nil
		}
	}
	return Input{}, fmt.Errorf("upstream probe: %s not in the resulting lock", name)
}
