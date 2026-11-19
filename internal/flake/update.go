package flake

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
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
	if err := validateNames(names, beforeByName, before); err != nil {
		return nil, err
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

// UpdateReporter observes UpdateSeq as it happens so a caller can render live
// progress between lock writes. Callbacks arrive on the calling goroutine, in
// order: one Plan after validation, then Start/Done per input.
type UpdateReporter interface {
	Plan(names []string) // the validated update order, before anything runs
	Start(name string)   // this input's `nix flake update` is beginning
	Done(r UpdateResult) // its outcome has landed in flake.lock
}

// UpdateSeq is Update's streaming counterpart: it moves the same inputs, but
// one `nix flake update <name>` invocation at a time so rep can show progress
// while the slow upstream fetches run. With no names it updates every
// non-follows input (UpdateAll's set). The trade is per-input nix startup
// overhead for feedback, plus weaker atomicity: it stops at the first failure,
// returning what already landed alongside the error, and inputs after the
// failing one are left untouched.
func (c *Client) UpdateSeq(ctx context.Context, rep UpdateReporter, names ...string) ([]UpdateResult, error) {
	before, err := c.Inputs(ctx)
	if err != nil {
		return nil, err
	}
	// "Every input" is every input nix can be asked for by name. One whose
	// name is not an input name by nix's rule is left as it was, and the run
	// ends by saying so (a name given explicitly is refused before anything
	// runs, by validateNames).
	var left []string
	if len(names) == 0 {
		for _, in := range before {
			switch {
			case in.Follows:
			case !IsInputName(in.Name):
				left = append(left, in.Name)
			default:
				names = append(names, in.Name) // Inputs is already name-sorted
			}
		}
	}
	cur := indexByName(before)
	if err := validateNames(names, cur, before); err != nil {
		return nil, err
	}
	rep.Plan(names)

	results := make([]UpdateResult, 0, len(names))
	for _, n := range names {
		rep.Start(n)
		res, runErr := c.run(ctx, "nix", c.updateArgs([]string{n})...)
		if runErr != nil {
			return results, fmt.Errorf("updating %s: %w", n, runErr)
		}
		after, err := c.lockInputs()
		if err != nil {
			return results, err
		}
		r := buildResults([]string{n}, cur, after, updatedBlocks(string(res.stderr)))[0]
		results = append(results, r)
		rep.Done(r)
		cur = after // this input's after is the next input's before
	}
	if len(left) > 0 {
		return results, fmt.Errorf("not updated: %w", &InvalidInputError{Names: left})
	}
	return results, nil
}

// lockInputs reads root/flake.lock straight from disk and indexes its inputs
// by name. UpdateSeq calls it after every per-input lock write, where a full
// `nix flake metadata` round-trip per input would be waste; the pre-update
// snapshot still comes from Inputs, which also sees inputs flake.nix declares
// but the lock does not yet hold.
func (c *Client) lockInputs() (map[string]Input, error) {
	data, err := os.ReadFile(filepath.Join(c.root, "flake.lock"))
	if err != nil {
		return nil, fmt.Errorf("read flake.lock: %w", err)
	}
	md, err := parseLockFile(data)
	if err != nil {
		return nil, err
	}
	inputs, err := md.buildInputs()
	if err != nil {
		return nil, err
	}
	return indexByName(inputs), nil
}

// validateNames rejects names that are not input names by nix's rule, unknown
// inputs and `follows` aliases before any nix invocation, so a typo yields a
// friendly list instead of a cryptic nix error and never a partial write.
//
// The rule comes first and does not depend on the lock: a lock can hold an
// input named like an option, and being in the lock must not be what lets a
// name through to nix's command line.
func validateNames(names []string, byName map[string]Input, all []Input) error {
	if err := CheckInputNames(names...); err != nil {
		return err
	}
	var unknown, follows []string
	for _, n := range names {
		in, ok := byName[n]
		if !ok {
			unknown = append(unknown, n)
		} else if in.Follows {
			follows = append(follows, n)
		}
	}
	if len(unknown) > 0 {
		return &UnknownInputError{Name: strings.Join(unknown, ", "), Available: inputNames(all)}
	}
	if len(follows) > 0 {
		// A `follows` alias has no independent lock entry; updating it is a nix
		// error. Point the user at the input it actually follows.
		return fmt.Errorf("%s is a `follows` alias with no independent lock; update the input it follows instead", strings.Join(follows, ", "))
	}
	return nil
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
// flake.lock. Modern positional form (Nix ≥ 2.19): `nix flake update -- <input…>`;
// with no names it updates every input.
//
// The names stand after `--`, which ends nix's options: whatever a name looks
// like, nix reads it as a name. Callers have already refused every name that
// is not one by nix's rule (validateNames), so this is the second of two
// independent guards, and the one that still holds if that rule is ever
// loosened.
func (c *Client) updateArgs(names []string) []string {
	args := nixArgs("flake", "update")
	if len(names) == 0 {
		return args
	}
	return append(append(args, "--"), names...)
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
