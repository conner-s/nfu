package main

import (
	"reflect"
	"testing"

	"github.com/conner-s/nfu/internal/config"
)

func storedProject() config.Project {
	f := false
	return config.Project{
		Inputs:      []string{"totality"},
		Hosts:       []string{"Fabricator"},
		LocalSwitch: &f,
		Description: "keep me",
	}
}

// `add` builds from flags alone; an empty base makes that the same call.
func TestApplyFieldFlagsBuildsFromFlags(t *testing.T) {
	g := globalFlags{inputs: "totality, npk", hosts: "Fabricator,Servitor", hostArgs: stringList{"Fabricator:--magic-rollback false"}}
	p, defs, err := applyFieldFlags(g, config.Project{})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if !reflect.DeepEqual(p.Inputs, []string{"totality", "npk"}) {
		t.Errorf("inputs = %v", p.Inputs)
	}
	if !reflect.DeepEqual(p.Hosts, []string{"Fabricator", "Servitor"}) {
		t.Errorf("hosts = %v", p.Hosts)
	}
	if !p.SwitchLocal() {
		t.Errorf("localSwitch should default to true")
	}
	if got := defs["Fabricator"].DeployArgs; !reflect.DeepEqual(got, []string{"--magic-rollback", "false"}) {
		t.Errorf("host args = %v", got)
	}
}

// `edit` changes only the fields it is given — that is the whole difference
// from `add`, which replaces the definition.
func TestApplyFieldFlagsMergesIntoStored(t *testing.T) {
	p, _, err := applyFieldFlags(globalFlags{hosts: "Servitor"}, storedProject())
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if !reflect.DeepEqual(p.Inputs, []string{"totality"}) {
		t.Errorf("inputs changed by a --hosts edit: %v", p.Inputs)
	}
	if !reflect.DeepEqual(p.Hosts, []string{"Servitor"}) {
		t.Errorf("hosts = %v", p.Hosts)
	}
	if p.Description != "keep me" {
		t.Errorf("description dropped: %q", p.Description)
	}
	if p.SwitchLocal() {
		t.Errorf("stored localSwitch=false was not preserved")
	}
}

// Rewriting the host list must not leave an inline def for a host the project
// no longer deploys.
func TestApplyFieldFlagsPrunesInlineDefs(t *testing.T) {
	base := storedProject()
	base.HostDefs = map[string]config.Host{
		"Fabricator": {DeployArgs: []string{"--magic-rollback", "false"}},
		"Servitor":   {Hostname: "192.168.4.11"},
	}
	p, _, err := applyFieldFlags(globalFlags{hosts: "Servitor"}, base)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if _, ok := p.HostDefs["Fabricator"]; ok {
		t.Errorf("inline def outlived its host: %+v", p.HostDefs)
	}
	if p.HostDefs["Servitor"].Hostname != "192.168.4.11" {
		t.Errorf("still-deployed host lost its def: %+v", p.HostDefs)
	}
}

func TestApplyFieldFlagsLocalSwitch(t *testing.T) {
	// --local-switch puts a remote-only project back on the default (true).
	p, _, err := applyFieldFlags(globalFlags{localSwitch: true}, storedProject())
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if !p.SwitchLocal() || p.LocalSwitch != nil {
		t.Errorf("--local-switch: SwitchLocal=%v LocalSwitch=%v", p.SwitchLocal(), p.LocalSwitch)
	}

	// --no-local-switch is the other direction.
	p, _, err = applyFieldFlags(globalFlags{noLocalSwitch: true}, config.Project{Inputs: []string{"totality"}})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if p.SwitchLocal() {
		t.Errorf("--no-local-switch did not take")
	}

	// Both at once is a contradiction, not a silent winner.
	if _, _, err := applyFieldFlags(globalFlags{localSwitch: true, noLocalSwitch: true}, config.Project{}); err == nil {
		t.Errorf("--local-switch --no-local-switch should error")
	}
}

// Only definition flags send add/edit down the non-interactive path; the global
// ones (-y, --json, …) must still open the TUI.
func TestHasFieldFlags(t *testing.T) {
	cases := []struct {
		g    globalFlags
		want bool
	}{
		{globalFlags{}, false},
		{globalFlags{yes: true, asJSON: true}, false},
		{globalFlags{inputs: "totality"}, true},
		{globalFlags{hosts: "Servitor"}, true},
		{globalFlags{hostArgs: stringList{"H:--x"}}, true},
		{globalFlags{localSwitch: true}, true},
		{globalFlags{noLocalSwitch: true}, true},
	}
	for _, c := range cases {
		if got := hasFieldFlags(c.g); got != c.want {
			t.Errorf("hasFieldFlags(%+v) = %v, want %v", c.g, got, c.want)
		}
	}
}

func TestParseHostArgs(t *testing.T) {
	defs, err := parseHostArgs(stringList{"Fabricator:--magic-rollback false", "Servitor:"})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got := defs["Fabricator"].DeployArgs; !reflect.DeepEqual(got, []string{"--magic-rollback", "false"}) {
		t.Errorf("Fabricator = %v", got)
	}
	if got := defs["Servitor"].DeployArgs; len(got) != 0 {
		t.Errorf("Servitor = %v, want none", got)
	}
	if _, err := parseHostArgs(stringList{"nocolon"}); err == nil {
		t.Errorf("missing ':' should error")
	}
}
