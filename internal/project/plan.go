package project

import "fmt"

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
}

// BuildPlan resolves a named project against the catalog into a Plan. It is pure
// (no I/O) so the resolution — host-arg lookup, localSwitch default — is
// unit-testable. commit is the effective commit decision for the run.
func BuildPlan(c *Catalog, name string, commit bool) (Plan, error) {
	e, ok := c.Project(name)
	if !ok {
		return Plan{}, fmt.Errorf("unknown project %q", name)
	}
	p := e.Project
	if len(p.Inputs) == 0 {
		return Plan{}, fmt.Errorf("project %q has no inputs to update", name)
	}
	plan := Plan{
		Name:        name,
		Inputs:      p.Inputs,
		Commit:      commit,
		LocalSwitch: p.SwitchLocal(),
	}
	for _, host := range p.Hosts {
		def, _ := c.HostDef(p, host) // unknown host → zero Host → bare deploy
		plan.Hosts = append(plan.Hosts, HostStep{Name: host, Args: def.DeployArgs})
	}
	return plan, nil
}
