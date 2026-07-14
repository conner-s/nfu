package flake

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// Update moves the named top-level inputs to their latest locked pins by
// running a single `nix flake update <names…>`, and reports what actually
// changed. Names are validated against the flake's inputs first, so a typo
// yields a friendly list instead of a cryptic nix error and never a partial
// write.
//
// WARNING for callers: inputs are shared across every host a flake defines —
// updating nixpkgs moves all of them, not just the one you have in mind.
func (c *Client) Update(ctx context.Context, names ...string) ([]UpdateResult, error) {
	if len(names) == 0 {
		return nil, fmt.Errorf("no inputs to update")
	}
	before, err := c.Inputs(ctx)
	if err != nil {
		return nil, err
	}
	beforeByName := indexByName(before)

	var unknown, follows []string
	for _, n := range names {
		in, ok := beforeByName[n]
		if !ok {
			unknown = append(unknown, n)
		} else if in.Follows {
			follows = append(follows, n)
		}
	}
	if len(unknown) > 0 {
		return nil, &UnknownInputError{Name: strings.Join(unknown, ", "), Available: inputNames(before)}
	}
	if len(follows) > 0 {
		// A `follows` alias has no independent lock entry; updating it is a nix
		// error. Point the user at the input it actually follows.
		return nil, fmt.Errorf("%s is a `follows` alias with no independent lock; update the input it follows instead", strings.Join(follows, ", "))
	}

	res, runErr := c.run(ctx, "nix", c.updateArgs(names)...)
	// Read the post-update lock even on error: `nix flake update` can move some
	// inputs before failing on a later one, and we should report what landed.
	after, afterErr := c.Inputs(ctx)
	if afterErr != nil {
		if runErr != nil {
			return nil, runErr
		}
		return nil, afterErr
	}
	results := buildResults(names, beforeByName, indexByName(after), updatedBlocks(string(res.stderr)))
	if runErr != nil {
		return results, runErr
	}
	return results, nil
}

// UpdateAll updates every top-level input (`nix flake update` with no args) and
// reports each input's before/after pin.
func (c *Client) UpdateAll(ctx context.Context) ([]UpdateResult, error) {
	before, err := c.Inputs(ctx)
	if err != nil {
		return nil, err
	}
	// Report only real inputs. A `follows` alias resolves to the pin of the input
	// it follows, so it would otherwise appear as a phantom second "update" (and
	// inflate the count / commit message) whenever its target moves.
	var names []string
	for _, in := range before {
		if !in.Follows {
			names = append(names, in.Name)
		}
	}
	sort.Strings(names)
	beforeByName := indexByName(before)

	res, runErr := c.run(ctx, "nix", c.updateArgs(nil)...)
	after, afterErr := c.Inputs(ctx)
	if afterErr != nil {
		if runErr != nil {
			return nil, runErr
		}
		return nil, afterErr
	}
	results := buildResults(names, beforeByName, indexByName(after), updatedBlocks(string(res.stderr)))
	if runErr != nil {
		return results, runErr
	}
	return results, nil
}

// buildResults pairs each name's before/after pin into an UpdateResult. It is
// pure so the diff logic (moved vs. already-current) is unit-testable.
func buildResults(names []string, before, after map[string]Input, blocks map[string]string) []UpdateResult {
	results := make([]UpdateResult, 0, len(names))
	for _, n := range names {
		b := before[n]
		a := after[n]
		results = append(results, UpdateResult{
			Input:      n,
			OldPin:     b.Pin(),
			NewPin:     a.Pin(),
			OldMod:     b.LastModified,
			NewMod:     a.LastModified,
			Transition: blocks[n],
			Unchanged:  a.Pin() == b.Pin(),
		})
	}
	return results
}

// updateArgs builds the argv for the ONE nix invocation nfu uses to write
// flake.lock. Modern positional form (Nix ≥ 2.19): `nix flake update <input…>`;
// with no names it updates every input.
func (c *Client) updateArgs(names []string) []string {
	args := nixArgs("flake", "update")
	return append(args, names...)
}

func indexByName(inputs []Input) map[string]Input {
	m := make(map[string]Input, len(inputs))
	for _, in := range inputs {
		m[in.Name] = in
	}
	return m
}

// updatedBlocks splits `nix flake update` stderr into the "• Updated input
// 'NAME':" blocks, keyed by input name. Each block runs from its header to the
// next header (or end), verbatim-ish (trimmed), so the caller can echo nix's own
// old→new source lines.
func updatedBlocks(stderr string) map[string]string {
	blocks := map[string]string{}
	var name string
	var cur []string
	flush := func() {
		if name != "" {
			blocks[name] = strings.TrimRight(strings.Join(cur, "\n"), "\n")
		}
		name, cur = "", nil
	}
	for _, ln := range strings.Split(stderr, "\n") {
		if n, ok := parseUpdatedHeader(ln); ok {
			flush()
			name = n
			cur = []string{strings.TrimSpace(ln)}
			continue
		}
		if name != "" {
			if t := strings.TrimSpace(ln); t != "" {
				cur = append(cur, t)
			}
		}
	}
	flush()
	return blocks
}

// parseUpdatedHeader returns the input name from a "… Updated input 'NAME':"
// line (the bullet and coloring vary across nix versions).
func parseUpdatedHeader(line string) (string, bool) {
	const marker = "Updated input '"
	i := strings.Index(line, marker)
	if i < 0 {
		return "", false
	}
	rest := line[i+len(marker):]
	j := strings.IndexByte(rest, '\'')
	if j < 0 {
		return "", false
	}
	return rest[:j], true
}
