package tui

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/conner-s/nfu/internal/config"
	"github.com/conner-s/nfu/internal/flake"
	"github.com/conner-s/nfu/internal/project"
	"github.com/conner-s/nfu/internal/ui"
)

// A lock can hold an input named like an option (nix itself locks
// inputs."--refresh"). These tests hold the TUIs to never offering such a
// name to nix; internal/flake refuses it again on its own.

func flagNamedPicker() model {
	m := pickingModel()
	m.client = flake.New("/flake")
	m.inputs = []flake.Input{
		{Name: "--refresh", Rev: "aaa"},
		{Name: "nixpkgs", Rev: "bbb"},
		{Name: "-n", Rev: "ccc"},
		{Name: "we ird", Rev: "ddd"},
	}
	return m
}

func TestPickerNeverSelectsAnInputNixCannotName(t *testing.T) {
	m := flagNamedPicker()
	if got := firstSelectable(m.inputs); got != 1 {
		t.Errorf("the cursor starts on row %d, want 1 (nixpkgs)", got)
	}

	// space on the flag-named row does nothing.
	m.cursor = 0
	m = step(m, key(" "))
	if m.selected[0] || len(m.selectedNames()) != 0 {
		t.Errorf("space selected an input named --refresh: %v", m.selectedNames())
	}

	// `a` selects what can be updated, and only that.
	m = step(m, key("a"))
	if got := m.selectedNames(); len(got) != 1 || got[0] != "nixpkgs" {
		t.Errorf("select-all picked %q, want nixpkgs alone", got)
	}

	// Even a selection forced into the model does not reach the names.
	m.selected[0], m.selected[2], m.selected[3] = true, true, true
	if got := m.selectedNames(); len(got) != 1 || got[0] != "nixpkgs" {
		t.Errorf("selectedNames = %q, want nixpkgs alone", got)
	}
}

func TestPickerNeverProbesAnInputNixCannotName(t *testing.T) {
	m := flagNamedPicker()
	m.cursor = 0
	next, cmd := m.Update(key("f"))
	if cmd != nil || len(next.(model).checking) != 0 {
		t.Error("f on an input named --refresh started a probe")
	}
	m = step(m, key("F"))
	if len(m.checking) != 1 || !m.checking["nixpkgs"] {
		t.Errorf("F probes %v, want nixpkgs alone", m.checking)
	}
}

func TestPickerSaysWhyOnTheRow(t *testing.T) {
	lines := strings.Split(flagNamedPicker().pickView(), "\n")
	var row string
	for _, l := range lines {
		if strings.Contains(l, "--refresh") {
			row = l
		}
	}
	if !strings.Contains(row, "[!]") || !strings.Contains(row, "nix updates only names of ASCII letters, digits, _ and -") {
		t.Errorf("the row of an input named --refresh does not say why it cannot be picked: %q", row)
	}
}

// The form lists a flake's inputs for a project. One nix cannot update by
// name cannot be ticked; one a stored project already names can be unticked.
func TestFormRefusesToTickAnInputNixCannotName(t *testing.T) {
	inputs := []flake.Input{{Name: "--refresh"}, {Name: "nixpkgs"}}
	m := newFormModel(inputs, nil, FormStart{}, ui.NewStyles())
	m.section = secInputs
	m = formStep(m, keySpace)
	if m.inSel["--refresh"] {
		t.Error("the form ticked an input named --refresh")
	}
	if !strings.Contains(m.err, "nix cannot update an input with that name") {
		t.Errorf("the form does not say why: %q", m.err)
	}

	stored := EditStart("lab", config.Project{Inputs: []string{"--refresh", "nixpkgs"}})
	m = newFormModel(inputs, nil, stored, ui.NewStyles())
	m.section = secInputs
	m = formStep(m, keySpace)
	if m.inSel["--refresh"] {
		t.Error("a stored input named --refresh could not be unticked")
	}
}

// A project file that names such an input gets no plan in the manager: no dry
// run, no run, and the reason as a toast.
func TestManagerRefusesAPlanWithAFlagNamedInput(t *testing.T) {
	cfg := &config.Config{Projects: map[string]config.Project{
		"lab": {Inputs: []string{"nixpkgs", "--refresh"}},
	}}
	for _, dry := range []bool{true, false} {
		m := testManager()
		m.deps.Client = flake.New("/flake")
		m.deps.Catalog = project.BuildCatalog(cfg, &project.Index{}, nil)
		m.entries = m.deps.Catalog.List()
		next, _ := m.startRun(dry)
		got := next.(managerModel)
		if got.run != nil {
			t.Errorf("startRun(dry=%v) made a run of a project with an input named --refresh", dry)
		}
		if !strings.Contains(got.toast.String(), `"--refresh" is not a name nix can update`) {
			t.Errorf("startRun(dry=%v) does not say why: %q", dry, got.toast)
		}
	}
}

// The editor gets an absolute path. With a relative projects directory, a
// file whose name starts with a dash would otherwise reach it as an option.
func TestEditorPathIsAbsolute(t *testing.T) {
	for _, dir := range []string{".", "", "projects", "-p", "/etc/nfu/projects"} {
		for _, name := range []string{"lab", "-c:!sh", "--version", "+cmd"} {
			path, err := projectFilePath(dir, name)
			if err != nil {
				t.Errorf("projectFilePath(%q, %q): %v", dir, name, err)
				continue
			}
			if !filepath.IsAbs(path) || filepath.Base(path) != name+".toml" {
				t.Errorf("projectFilePath(%q, %q) = %q, want an absolute path to %s.toml", dir, name, path, name)
			}
		}
	}
}
