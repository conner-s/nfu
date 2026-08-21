package main

import (
	"fmt"
	"strings"

	"github.com/conner-s/nfu/internal/project"
)

// setPrefix marks a positional as a project reference rather than a flake input:
// `nfu @workstation` updates every input that project (and everything it
// `includes`) names. The sigil is what keeps this free of the reserved-word
// problem the bare verbs have — no flake input can be called `@workstation`, so
// nothing new is reserved and `nfu update <name>` stays the only escape hatch
// anyone has to remember.
const setPrefix = "@"

// expandSetRefs rewrites `@<project>` positionals into the flake inputs they
// stand for, leaving plain input names alone. It is the layer-1 view of a
// project: the same bundle `nfu project run` would update, switch, and deploy,
// but here it only moves the lock.
//
// Names are deduplicated across the whole expansion in first-appearance order,
// so `nfu @workstation npk` — where the bundle already covers npk — is one lock
// write with npk named once, not a doubled entry in the update list.
//
// The catalog is loaded only when a reference is actually present, so the plain
// `nfu <input>` path never pays for reading the projects directory (and never
// fails on a broken project file it was not asked about).
func expandSetRefs(g globalFlags, names []string) ([]string, error) {
	if !hasSetRef(names) {
		return names, nil
	}
	_, cat, _, err := loadCatalog(g)
	if err != nil {
		return nil, err
	}

	out := make([]string, 0, len(names))
	seen := map[string]bool{}
	add := func(n string) {
		if !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	for _, n := range names {
		ref, ok := strings.CutPrefix(n, setPrefix)
		if !ok {
			add(n)
			continue
		}
		if ref == "" {
			return nil, fmt.Errorf("%q names no project: write `@<project>` (e.g. `nfu @workstation`)", n)
		}
		inputs, err := project.ResolveInputs(cat, ref)
		if err != nil {
			return nil, err
		}
		for _, in := range inputs {
			add(in)
		}
	}
	return out, nil
}

// hasSetRef reports whether any positional is a `@<project>` reference.
func hasSetRef(names []string) bool {
	for _, n := range names {
		if strings.HasPrefix(n, setPrefix) {
			return true
		}
	}
	return false
}
