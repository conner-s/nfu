package project

import (
	"fmt"
	"strings"

	"github.com/conner-s/nfu/internal/config"
)

// Resolved is one project flattened through its `includes` graph: the union of
// every member's inputs and deploy hosts, and the single local-switch decision
// the merged pipeline runs with. It is what a plan is built from, and what
// `nfu @<project>` expands to.
//
// Order is first-appearance, root before its includes, depth-first: a bundle's
// own inputs lead, then each included project's in the order it is named. That
// makes the update line stable and readable rather than sorted-by-accident.
type Resolved struct {
	Inputs      []string
	Hosts       []HostStep
	LocalSwitch bool

	// Members lists every project the flattening walked, root first — what the
	// plan preview names so a merged run says where its inputs came from.
	Members []string
}

// Resolve flattens name through its includes graph against the catalog.
//
// LocalSwitch follows one rule, in this order: an explicit `localSwitch` on the
// ROOT project always wins (a bundle that says false stays false, whatever its
// members want); otherwise a plain project defaults to true; otherwise — an
// unset bundle — it is true when ANY member switches locally, which is the
// point of bundling (rebuild once, for everyone who needed a rebuild).
//
// A cycle is an error naming the path, so `includes` can never hang or recurse
// forever. A diamond (two members including the same project) is NOT a cycle:
// the shared project contributes once. An include naming a project the catalog
// does not have is an error too — silently dropping it would quietly shrink
// what a bundle updates.
func Resolve(c *Catalog, name string) (Resolved, error) {
	root, ok := c.Project(name)
	if !ok {
		return Resolved{}, fmt.Errorf("unknown project %q", name)
	}
	var r Resolved
	seenInput := map[string]bool{}
	seenHost := map[string]bool{}
	visited := map[string]bool{}
	memberSwitch := false // does any INCLUDED project want a local switch?

	var walk func(n string, p config.Project, path []string) error
	walk = func(n string, p config.Project, path []string) error {
		visited[n] = true
		r.Members = append(r.Members, n)
		for _, in := range p.Inputs {
			if !seenInput[in] {
				seenInput[in] = true
				r.Inputs = append(r.Inputs, in)
			}
		}
		for _, h := range p.Hosts {
			if seenHost[h] {
				continue // already deployed by an earlier member; deploy once
			}
			seenHost[h] = true
			// Host args resolve against the project that NAMED the host, so an
			// included project's inline hostDefs still apply inside a bundle.
			def, _ := c.HostDef(p, h)
			r.Hosts = append(r.Hosts, HostStep{Name: h, Args: def.DeployArgs})
		}
		for _, inc := range p.Includes {
			// Cycle before dedup: a repeat on the CURRENT branch is a cycle, a
			// repeat on a sibling branch is just a diamond.
			if pos := indexOf(path, inc); pos >= 0 {
				return fmt.Errorf("include cycle: %s", strings.Join(concat(path[pos:], inc), " → "))
			}
			e, ok := c.Project(inc)
			if !ok {
				return fmt.Errorf("project %q includes unknown project %q; "+
					"`nfu project list` shows what is defined", n, inc)
			}
			if e.Project.SwitchLocal() {
				memberSwitch = true
			}
			if visited[inc] {
				continue
			}
			if err := walk(inc, e.Project, concat(path, inc)); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk(name, root.Project, []string{name}); err != nil {
		return Resolved{}, err
	}

	switch {
	case root.Project.LocalSwitch != nil:
		r.LocalSwitch = *root.Project.LocalSwitch
	case len(root.Project.Includes) == 0:
		r.LocalSwitch = true
	default:
		r.LocalSwitch = memberSwitch
	}
	return r, nil
}

// ResolveInputs is Resolve's narrow form for the update-only paths (`nfu
// @<project>`), which want the flattened input list and nothing else.
func ResolveInputs(c *Catalog, name string) ([]string, error) {
	r, err := Resolve(c, name)
	if err != nil {
		return nil, err
	}
	if len(r.Inputs) == 0 {
		return nil, fmt.Errorf("project %q has no inputs to update", name)
	}
	return r.Inputs, nil
}

// concat copies ss and appends v. The copy matters: the walk hands a path down
// several sibling branches, and a shared backing array would let one branch
// overwrite another's ancestry (and the cycle check with it).
func concat(ss []string, v string) []string {
	out := make([]string, len(ss), len(ss)+1)
	copy(out, ss)
	return append(out, v)
}

func indexOf(ss []string, v string) int {
	for i, s := range ss {
		if s == v {
			return i
		}
	}
	return -1
}
