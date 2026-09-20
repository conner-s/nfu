package tui

import (
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/conner-s/nfu/internal/config"
	"github.com/conner-s/nfu/internal/flake"
	"github.com/conner-s/nfu/internal/ui"
)

func formStep(m formModel, msg tea.Msg) formModel {
	next, _ := m.Update(msg)
	return next.(formModel)
}

var (
	keyTab   = tea.KeyPressMsg{Code: tea.KeyTab}
	keySave  = tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl}
	keySpace = tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
)

func editForm(t *testing.T, base config.Project) formModel {
	t.Helper()
	inputs := []flake.Input{{Name: "totality"}, {Name: "npk"}, {Name: "mirror", Follows: true}}
	known := []HostOption{{Name: "Servitor"}}
	return newFormModel(inputs, known, EditStart("totality", base), ui.NewStyles())
}

// The form only shows four fields; an edit must carry the rest of the file
// through untouched rather than quietly dropping it.
func TestEditCarriesFieldsTheFormDoesNotShow(t *testing.T) {
	base := config.Project{
		Inputs:      []string{"totality"},
		Hosts:       []string{"Fabricator"},
		Description: "push totality everywhere",
		FlakeRoot:   "~/Projects/nixos-config",
		HostDefs: map[string]config.Host{
			"Fabricator": {Hostname: "192.168.4.10", DeployArgs: []string{"--magic-rollback", "false"}},
		},
	}
	m := editForm(t, base)

	// The seed reflects what is stored: inputs ticked, hosts ticked, and the
	// inline def's args shown next to a host the shared catalog does not know.
	if !m.inSel["totality"] {
		t.Errorf("stored input not pre-selected: %v", m.inSel)
	}
	var fab *hostEntry
	for i := range m.hosts {
		if m.hosts[i].name == "Fabricator" {
			fab = &m.hosts[i]
		}
	}
	if fab == nil || !fab.selected {
		t.Fatalf("stored host not pre-selected: %+v", m.hosts)
	}
	if !reflect.DeepEqual(fab.args, []string{"--magic-rollback", "false"}) {
		t.Errorf("inline host args not seeded: %v", fab.args)
	}

	m = formStep(m, keyTab)   // → inputs
	m = formStep(m, key("j")) // → npk
	m = formStep(m, keySpace) // add it
	m = formStep(m, keySave)

	res := m.result
	if !res.Saved || res.Name != "totality" {
		t.Fatalf("save: %+v", res)
	}
	if got := res.Project.Inputs; !reflect.DeepEqual(got, []string{"totality", "npk"}) {
		t.Errorf("inputs = %v, want [totality npk]", got)
	}
	if res.Project.Description != base.Description || res.Project.FlakeRoot != base.FlakeRoot {
		t.Errorf("dropped description/flakeRoot: %+v", res.Project)
	}
	if got := res.Project.HostDefs["Fabricator"]; got.Hostname != "192.168.4.10" {
		t.Errorf("dropped inline host def: %+v", res.Project.HostDefs)
	}
	if !res.Project.SwitchLocal() {
		t.Errorf("localSwitch flipped by an edit that never touched it")
	}
}

// Dropping a host drops the inline def that only existed to configure it.
func TestEditDropsInlineDefWithItsHost(t *testing.T) {
	base := config.Project{
		Inputs:   []string{"totality"},
		Hosts:    []string{"Fabricator"},
		HostDefs: map[string]config.Host{"Fabricator": {DeployArgs: []string{"--magic-rollback", "false"}}},
	}
	m := editForm(t, base)
	m.section = secHosts
	for i, h := range m.hosts { // put the cursor on Fabricator, then untick it
		if h.name == "Fabricator" {
			m.hCursor = i
		}
	}
	m = formStep(m, keySpace)
	m = formStep(m, keySave)

	if got := m.result.Project.Hosts; len(got) != 0 {
		t.Fatalf("hosts = %v, want none", got)
	}
	if got := m.result.Project.HostDefs; len(got) != 0 {
		t.Errorf("inline def outlived its host: %+v", got)
	}
}

// Editing an existing project locks the name: the filename is its identity.
func TestEditLocksName(t *testing.T) {
	m := editForm(t, config.Project{Inputs: []string{"totality"}})
	m = formStep(m, key("x"))
	if m.name.Value() != "totality" {
		t.Errorf("name changed while editing: %q", m.name.Value())
	}
}

// A new project starts blank and defaults to switching the local host.
func TestNewFormDefaults(t *testing.T) {
	inputs := []flake.Input{{Name: "totality"}}
	m := newFormModel(inputs, nil, FormStart{}, ui.NewStyles())
	if m.editing || m.name.Value() != "" || !m.localSwitch {
		t.Errorf("new form: editing=%v name=%q localSwitch=%v", m.editing, m.name.Value(), m.localSwitch)
	}
	m = formStep(m, keySave) // no name, no inputs → refuses, stays open
	if m.quit || m.err == "" {
		t.Errorf("empty save should report an error, not quit: err=%q quit=%v", m.err, m.quit)
	}
}

// bubbletea v2 stringifies the space bar as "space", not " ". The local-switch
// toggle is the one switch arm no other test exercises.
func TestSpaceTogglesLocalSwitch(t *testing.T) {
	m := newFormModel([]flake.Input{{Name: "totality"}}, nil, FormStart{}, ui.NewStyles())
	if !m.localSwitch {
		t.Fatal("precondition: a new form switches the local host")
	}
	m.section = secSwitch
	m = formStep(m, keySpace)
	if m.localSwitch {
		t.Fatal("space did not toggle localSwitch off")
	}
	m = formStep(m, keySpace)
	if !m.localSwitch {
		t.Fatal("space did not toggle localSwitch back on")
	}
}

// bubbles v2 renders only the first placeholder rune when the input has no
// width (bubbles#950), which turned "project-name" into "p".
func TestPlaceholdersRenderInFull(t *testing.T) {
	m := newFormModel([]flake.Input{{Name: "totality"}}, nil, FormStart{}, ui.NewStyles())
	m.hostEdit = true
	m.hName.Focus()
	cases := map[string]string{
		"name": ansi.Strip(m.name.View()),
		"host": ansi.Strip(m.hName.View()),
		"args": ansi.Strip(m.hArgs.View()),
	}
	want := map[string]string{"name": "project-name", "host": "host-name", "args": "--magic-rollback false"}
	for field, got := range cases {
		if !strings.Contains(got, want[field]) {
			t.Errorf("%s placeholder = %q, want it to contain %q", field, got, want[field])
		}
	}
}

// Paste is its own message in bubbletea v2; under v1 it arrived as a key and
// reached the text input on its own.
func TestPasteLandsInTheFocusedInput(t *testing.T) {
	m := newFormModel([]flake.Input{{Name: "totality"}}, nil, FormStart{}, ui.NewStyles())
	m = formStep(m, tea.PasteMsg{Content: "abc"})
	if m.name.Value() != "abc" {
		t.Errorf("paste into the name field = %q, want %q", m.name.Value(), "abc")
	}

	m.hostEdit = true
	m.hName.Focus()
	m = formStep(m, tea.PasteMsg{Content: "Servitor"})
	if m.hName.Value() != "Servitor" {
		t.Errorf("paste into the host field = %q, want %q", m.hName.Value(), "Servitor")
	}
}
