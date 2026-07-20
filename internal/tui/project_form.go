// The project form (add/edit) TUI. Bare `nfu project add` opens a small sectioned
// builder: name, which flake inputs the project updates, whether it rebuilds the
// local host, and the deploy hosts (with per-host deploy args). It never touches
// the filesystem — RunProjectForm returns the built definition and the CLI
// persists it, mirroring how Run returns and lets the caller commit.
package tui

import (
	"context"
	"errors"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/conner-s/nfu/internal/config"
	"github.com/conner-s/nfu/internal/flake"
	"github.com/conner-s/nfu/internal/ui"
)

// HostOption is a known deploy host offered for selection (from the catalog).
type HostOption struct {
	Name string
	Args []string
}

// FormStart seeds the form when editing an existing project.
type FormStart struct {
	Name        string
	Inputs      []string
	Hosts       []string
	LocalSwitch bool
	Editing     bool // an existing project (name is fixed)
}

// FormResult is what the form produced.
type FormResult struct {
	Saved    bool
	Name     string
	Project  config.Project
	HostDefs map[string]config.Host // defs to persist for custom/edited hosts
}

type section int

const (
	secName section = iota
	secInputs
	secSwitch
	secHosts
	sectionCount
)

// hostEntry is one row of the hosts editor.
type hostEntry struct {
	name     string
	args     []string
	selected bool
	known    bool // came from the catalog (deselect, don't delete)
	edited   bool // args changed here, or a custom host → persist a def
}

type formModel struct {
	styles  ui.Styles
	section section

	name    textinput.Model
	editing bool // editing an existing project (name locked)

	inputs   []flake.Input
	inCursor int
	inSel    map[string]bool

	localSwitch bool

	hosts   []hostEntry
	hCursor int

	// host add/edit sub-mode
	hostEdit bool
	editIdx  int // -1 while adding a new host
	hName    textinput.Model
	hArgs    textinput.Model

	err    string
	result FormResult
	quit   bool
}

// RunProjectForm loads the flake's inputs, runs the builder, and returns the
// built definition. known seeds the host list; start seeds an edit.
func RunProjectForm(ctx context.Context, client *flake.Client, known []HostOption, start FormStart, s ui.Styles) (FormResult, error) {
	inputs, err := client.Inputs(ctx)
	if err != nil {
		return FormResult{}, err
	}
	m := newFormModel(inputs, known, start, s)
	final, err := tea.NewProgram(m, tea.WithContext(ctx)).Run()
	if err != nil && !errors.Is(err, tea.ErrProgramKilled) && !errors.Is(err, tea.ErrInterrupted) {
		return FormResult{}, err
	}
	if ctx.Err() != nil {
		return FormResult{}, nil
	}
	if fm, ok := final.(formModel); ok {
		return fm.result, nil
	}
	return FormResult{}, nil
}

func newFormModel(inputs []flake.Input, known []HostOption, start FormStart, s ui.Styles) formModel {
	name := textinput.New()
	name.Placeholder = "project-name"
	name.Prompt = ""
	name.SetValue(start.Name)
	name.Focus()

	sel := map[string]bool{}
	for _, n := range start.Inputs {
		sel[n] = true
	}

	// Build the host list: catalog hosts, plus any project hosts not in it.
	wanted := map[string]bool{}
	for _, h := range start.Hosts {
		wanted[h] = true
	}
	var hosts []hostEntry
	seen := map[string]bool{}
	for _, k := range known {
		hosts = append(hosts, hostEntry{name: k.Name, args: k.Args, selected: wanted[k.Name], known: true})
		seen[k.Name] = true
	}
	sort.Slice(hosts, func(i, j int) bool { return hosts[i].name < hosts[j].name })
	for _, h := range start.Hosts {
		if !seen[h] {
			hosts = append(hosts, hostEntry{name: h, selected: true, known: false})
			seen[h] = true
		}
	}

	hn := textinput.New()
	hn.Placeholder = "host-name"
	hn.Prompt = ""
	ha := textinput.New()
	ha.Placeholder = "--magic-rollback false"
	ha.Prompt = ""

	localSwitch := true
	if start.Editing {
		localSwitch = start.LocalSwitch
	}

	return formModel{
		styles:      s,
		section:     secName,
		name:        name,
		editing:     start.Editing,
		inputs:      inputs,
		inSel:       sel,
		localSwitch: localSwitch,
		hosts:       hosts,
		editIdx:     -1,
		hName:       hn,
		hArgs:       ha,
	}
}

func (m formModel) Init() tea.Cmd { return textinput.Blink }

func (m formModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, isKey := msg.(tea.KeyMsg)
	if !isKey {
		return m, nil
	}

	// The host add/edit sub-mode captures keys first.
	if m.hostEdit {
		return m.updateHostEdit(key)
	}

	switch key.String() {
	case "ctrl+c", "esc":
		m.quit = true
		return m, tea.Quit
	case "ctrl+s":
		return m.save()
	case "tab", "shift+tab":
		m.err = ""
		if m.section == secName {
			m.name.Blur()
		}
		if key.String() == "tab" {
			m.section = (m.section + 1) % sectionCount
		} else {
			m.section = (m.section + sectionCount - 1) % sectionCount
		}
		if m.section == secName {
			m.name.Focus()
			return m, textinput.Blink
		}
		return m, nil
	}

	switch m.section {
	case secName:
		var cmd tea.Cmd
		if m.editing {
			return m, nil // name is locked when editing
		}
		m.name, cmd = m.name.Update(key)
		return m, cmd
	case secInputs:
		return m.updateInputs(key)
	case secSwitch:
		switch key.String() {
		case " ", "enter", "left", "right", "h", "l":
			m.localSwitch = !m.localSwitch
		}
		return m, nil
	case secHosts:
		return m.updateHosts(key)
	}
	return m, nil
}

func (m formModel) updateInputs(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "up", "k":
		m.inCursor = clamp(m.inCursor-1, 0, len(m.selectableInputs())-1)
	case "down", "j":
		m.inCursor = clamp(m.inCursor+1, 0, len(m.selectableInputs())-1)
	case " ", "x":
		sel := m.selectableInputs()
		if m.inCursor < len(sel) {
			n := sel[m.inCursor].Name
			m.inSel[n] = !m.inSel[n]
		}
	}
	return m, nil
}

func (m formModel) updateHosts(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "up", "k":
		m.hCursor = clamp(m.hCursor-1, 0, len(m.hosts)-1)
	case "down", "j":
		m.hCursor = clamp(m.hCursor+1, 0, len(m.hosts)-1)
	case " ", "x":
		if m.hCursor < len(m.hosts) {
			m.hosts[m.hCursor].selected = !m.hosts[m.hCursor].selected
		}
	case "a":
		m.hostEdit = true
		m.editIdx = -1
		m.hName.SetValue("")
		m.hArgs.SetValue("")
		m.hName.Focus()
		m.hArgs.Blur()
		return m, textinput.Blink
	case "e":
		if m.hCursor < len(m.hosts) {
			m.hostEdit = true
			m.editIdx = m.hCursor
			m.hName.SetValue(m.hosts[m.hCursor].name)
			m.hArgs.SetValue(joinArgs(m.hosts[m.hCursor].args))
			m.hName.Blur() // name fixed when editing an entry
			m.hArgs.Focus()
			return m, textinput.Blink
		}
	case "d":
		if m.hCursor < len(m.hosts) && !m.hosts[m.hCursor].known {
			m.hosts = append(m.hosts[:m.hCursor], m.hosts[m.hCursor+1:]...)
			m.hCursor = clamp(m.hCursor, 0, len(m.hosts)-1)
		}
	}
	return m, nil
}

func (m formModel) updateHostEdit(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "esc":
		m.hostEdit = false
		return m, nil
	case "tab":
		if m.hName.Focused() {
			m.hName.Blur()
			m.hArgs.Focus()
		} else {
			m.hArgs.Blur()
			m.hName.Focus()
		}
		return m, textinput.Blink
	case "enter":
		return m.commitHostEdit()
	}
	var cmd tea.Cmd
	if m.hName.Focused() {
		m.hName, cmd = m.hName.Update(key)
	} else {
		m.hArgs, cmd = m.hArgs.Update(key)
	}
	return m, cmd
}

func (m formModel) commitHostEdit() (tea.Model, tea.Cmd) {
	args := splitArgs(m.hArgs.Value())
	if m.editIdx >= 0 { // editing an existing entry's args
		m.hosts[m.editIdx].args = args
		m.hosts[m.editIdx].edited = true
		m.hosts[m.editIdx].selected = true
	} else { // adding a new host
		name := trimSpace(m.hName.Value())
		if name == "" {
			m.hostEdit = false
			return m, nil
		}
		for i := range m.hosts {
			if m.hosts[i].name == name { // merge into existing
				m.hosts[i].args = args
				m.hosts[i].edited = true
				m.hosts[i].selected = true
				m.hostEdit = false
				return m, nil
			}
		}
		m.hosts = append(m.hosts, hostEntry{name: name, args: args, selected: true, known: false, edited: len(args) > 0})
	}
	m.hostEdit = false
	return m, nil
}

// save validates and finalizes the definition, quitting on success.
func (m formModel) save() (tea.Model, tea.Cmd) {
	name := trimSpace(m.name.Value())
	if err := validateFormName(name); err != "" {
		m.err = err
		return m, nil
	}
	inputs := m.selectedInputNames()
	if len(inputs) == 0 {
		m.err = "select at least one input (space to toggle)"
		return m, nil
	}

	p := config.Project{Inputs: inputs}
	defs := map[string]config.Host{}
	for _, h := range m.hosts {
		if !h.selected {
			continue
		}
		p.Hosts = append(p.Hosts, h.name)
		if len(h.args) > 0 && (!h.known || h.edited) {
			defs[h.name] = config.Host{DeployArgs: h.args}
		}
	}
	if !m.localSwitch {
		f := false
		p.LocalSwitch = &f
	}
	m.result = FormResult{Saved: true, Name: name, Project: p, HostDefs: defs}
	m.quit = true
	return m, tea.Quit
}

// selectableInputs is the inputs a project may update (follows aliases excluded).
func (m formModel) selectableInputs() []flake.Input {
	out := make([]flake.Input, 0, len(m.inputs))
	for _, in := range m.inputs {
		if !in.Follows {
			out = append(out, in)
		}
	}
	return out
}

func (m formModel) selectedInputNames() []string {
	var out []string
	for _, in := range m.inputs {
		if in.Follows {
			continue
		}
		if m.inSel[in.Name] {
			out = append(out, in.Name)
		}
	}
	return out
}

func joinArgs(a []string) string  { return strings.Join(a, " ") }
func splitArgs(s string) []string { return strings.Fields(s) }
func trimSpace(s string) string   { return strings.TrimSpace(s) }

// validateFormName mirrors project.ValidateName for inline UX feedback,
// returning "" when ok or a short message otherwise.
func validateFormName(name string) string {
	if name == "" {
		return "name is required"
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
		default:
			return "name: use letters, digits, '-', '_', '.'"
		}
	}
	return ""
}
