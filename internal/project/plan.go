package project

import (
	"fmt"
	"strings"

	"github.com/conner-s/nfu/internal/flake"
)

// HostStep is one deploy target in a plan: the host name plus the resolved extra
// `deploy` arguments (e.g. --magic-rollback false), nil for a bare deploy.
type HostStep struct {
	Name string
	Args []string
}

// Argv is the argument vector for this step's `deploy` invocation: the extra
// deploy args followed by the `.#<host>` installable.
func (h HostStep) Argv() []string {
	return append(append([]string{}, h.Args...), ".#"+h.Name)
}

// Plan is a fully-resolved run: exactly what update/switch/deploy will do, with
// no further config lookups. It is what --dry-run renders and what the confirm
// prompt describes.
type Plan struct {
	Name        string
	Inputs      []string
	Commit      bool
	LocalSwitch bool
	Hosts       []HostStep

	// Members names the projects this plan came from when it is more than one —
	// a bundle's `includes`, or the projects a --merge run folded together. Empty
	// for a plain single project, so the preview only explains itself when there
	// is something to explain.
	Members []string
}

// BuildPlan resolves a named project against the catalog into a Plan, flattening
// its `includes` graph on the way (see Resolve). It is pure (no I/O) so the
// resolution — host-arg lookup, localSwitch default, the include union — is
// unit-testable. commit is the effective commit decision for the run.
func BuildPlan(c *Catalog, name string, commit bool) (Plan, error) {
	r, err := Resolve(c, name)
	if err != nil {
		return Plan{}, err
	}
	if len(r.Inputs) == 0 {
		return Plan{}, fmt.Errorf("project %q has no inputs to update", name)
	}
	// A plan is what nix will be asked to do, so an input nix cannot be asked
	// for by name makes it no plan at all: it is refused here, where the dry
	// run, the confirm prompt and the manager all get it from, and again by
	// internal/flake when a run reaches it.
	if err := flake.CheckInputNames(r.Inputs...); err != nil {
		return Plan{}, fmt.Errorf("project %q: %w", name, err)
	}
	plan := Plan{
		Name:        name,
		Inputs:      r.Inputs,
		Commit:      commit,
		LocalSwitch: r.LocalSwitch,
		Hosts:       r.Hosts,
	}
	if len(r.Members) > 1 {
		plan.Members = r.Members
	}
	return plan, nil
}

// MergePlans folds several plans into ONE pipeline: the union of their inputs in
// a single lock write, one local switch if any of them wanted one, and each
// deploy host once in first-appearance order. It is what `nfu project run a b c
// --merge` runs, and the reason that command rebuilds once instead of N times.
//
// A host named by more than one plan keeps the FIRST plan's deploy args, on the
// same first-appearance principle as the ordering; plans disagreeing about a
// host's args is a config problem the shared catalog exists to prevent.
func MergePlans(plans []Plan, commit bool) Plan {
	merged := Plan{Commit: commit}
	seenInput, seenHost := map[string]bool{}, map[string]bool{}
	names := make([]string, 0, len(plans))
	for _, p := range plans {
		names = append(names, p.Name)
		merged.LocalSwitch = merged.LocalSwitch || p.LocalSwitch
		for _, in := range p.Inputs {
			if !seenInput[in] {
				seenInput[in] = true
				merged.Inputs = append(merged.Inputs, in)
			}
		}
		for _, h := range p.Hosts {
			if !seenHost[h.Name] {
				seenHost[h.Name] = true
				merged.Hosts = append(merged.Hosts, h)
			}
		}
	}
	merged.Name = strings.Join(names, "+")
	if len(names) > 1 {
		merged.Members = names
	}
	return merged
}

// HostNames flattens deploy steps to bare host names, for the listings that show
// where a project deploys but not with which arguments.
func HostNames(steps []HostStep) []string {
	if len(steps) == 0 {
		return nil
	}
	out := make([]string, 0, len(steps))
	for _, h := range steps {
		out = append(out, h.Name)
	}
	return out
}
