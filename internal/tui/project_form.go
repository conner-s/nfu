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

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"forgejo.kube.internal.conn3r.xyz/connerlab/termsafe"

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

	// Base is the stored definition being edited. The form only shows the four
	// fields above; everything else the file already said (description,
	// flakeRoot, inline host defs) is carried through from here, so an edit
	// never silently drops what it does not display.
	Base config.Project
}

// EditStart builds the seed for editing an existing project entry.
func EditStart(name string, p config.Project) FormStart {
	return FormStart{
		Name: name, Inputs: p.Inputs, Hosts: p.Hosts,
		LocalSwitch: p.SwitchLocal(), Editing: true, Base: p,
	}
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
	editing bool           // editing an existing project (name locked)
	fixed   string         // that project's name, as its file has it
	base    config.Project // what the file said, for the fields this form does not show

	inputs   []flake.Input
	inCursor int
	inSel    map[string]bool
	inGone   map[string]bool // stored inputs the flake no longer has

	localSwitch bool

	hosts   []hostEntry
	hCursor int

	// host add/edit sub-mode
	hostEdit bool
	editIdx  int // -1 while adding a new host
	hName    textinput.Model
	hArgs    textinput.Model

	err string
	// notice is the warning that text put into one of the fields was cleaned
	// on the way in (ui.Typed). It stays up until the form closes.
	notice termsafe.Text
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
	// bubbles v2 renders only the first placeholder rune when no width is set
	// (upstream bubbles#950, still open in v2.2.1); v1 showed the whole string.
	name.SetWidth(40)
	// A name given on the command line goes into the field, where it is
	// edited and then used: it is cleaned like anything typed there. The name
	// of a project being edited is not put in the field at all (see fixed).
	var notice termsafe.Text
	if !start.Editing {
		var cleaned string
		cleaned, notice = ui.Typed("The project name given", start.Name)
		name.SetValue(cleaned)
	}
	name.Focus()

	sel := map[string]bool{}
	for _, n := range start.Inputs {
		sel[n] = true
	}

	// An input the stored project names but the flake no longer has (renamed or
	// removed upstream) would otherwise just not be listed — and saving would
	// drop it without a word. List it, ticked and flagged, so the edit is the
	// user's call.
	have := map[string]bool{}
	for _, in := range inputs {
		have[in.Name] = true
	}
	missing := map[string]bool{}
	listed := append([]flake.Input(nil), inputs...)
	for _, n := range start.Inputs {
		if !have[n] {
			listed = append(listed, flake.Input{Name: n})
			missing[n] = true
		}
	}
	inputs = listed

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
			// A host the shared catalog does not know: its args, if any, come
			// from the project file's own inline def.
			hosts = append(hosts, hostEntry{name: h, args: start.Base.HostDefs[h].DeployArgs, selected: true, known: false})
			seen[h] = true
		}
	}

	hn := textinput.New()
	hn.Placeholder = "host-name"
	hn.Prompt = ""
	hn.SetWidth(32) // see the name field: bubbles#950
	ha := textinput.New()
	ha.Placeholder = "--magic-rollback false"
	ha.Prompt = ""
	ha.SetWidth(48) // see the name field: bubbles#950

	localSwitch := true
	if start.Editing {
		localSwitch = start.LocalSwitch
	}

	return formModel{
		styles:      s,
		section:     secName,
		name:        name,
		editing:     start.Editing,
		fixed:       start.Name,
		notice:      notice,
		base:        start.Base,
		inputs:      inputs,
		inSel:       sel,
		inGone:      missing,
		localSwitch: localSwitch,
		hosts:       hosts,
		editIdx:     -1,
		hName:       hn,
		hArgs:       ha,
	}
}

func (m formModel) Init() tea.Cmd { return textinput.Blink }

func (m formModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if paste, ok := msg.(tea.PasteMsg); ok {
		return m.updatePaste(paste)
	}
	key, isKey := msg.(tea.KeyPressMsg)
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
		m.name, cmd = m.name.Update(m.typed(key))
		return m, cmd
	case secInputs:
		return m.updateInputs(key)
	case secSwitch:
		switch key.String() {
		case "space", "enter", "left", "right", "h", "l":
			m.localSwitch = !m.localSwitch
		}
		return m, nil
	case secHosts:
		return m.updateHosts(key)
	}
	return m, nil
}

func (m formModel) updateInputs(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "up", "k":
		m.inCursor = clamp(m.inCursor-1, 0, len(m.selectableInputs())-1)
	case "down", "j":
		m.inCursor = clamp(m.inCursor+1, 0, len(m.selectableInputs())-1)
	case "space", "x":
		sel := m.selectableInputs()
		if m.inCursor < len(sel) {
			n := sel[m.inCursor].Name
			m.inSel[n] = !m.inSel[n]
		}
	}
	return m, nil
}

func (m formModel) updateHosts(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "up", "k":
		m.hCursor = clamp(m.hCursor-1, 0, len(m.hosts)-1)
	case "down", "j":
		m.hCursor = clamp(m.hCursor+1, 0, len(m.hosts)-1)
	case "space", "x":
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
			// The name is only shown here: confirming an edit keeps the
			// entry's own name. The arguments are edited and then saved, so
			// they are cleaned like typed text, and the notice says so when
			// what was stored is not what the field now holds.
			m.hName.SetValue(ui.Name(m.hosts[m.hCursor].name).String())
			args, warning := ui.Typed("The stored arguments of this host", joinArgs(m.hosts[m.hCursor].args))
			m.hArgs.SetValue(args)
			m.warn(warning)
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

// updatePaste forwards a bracketed paste to the focused text input. Under
// bubbletea v1 a paste arrived as an ordinary key message and fell through to
// the inputs on its own; v2 delivers tea.PasteMsg separately, so route it here
// or it is silently dropped.
func (m formModel) updatePaste(msg tea.PasteMsg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	cleaned, warning := ui.Typed("The pasted text", msg.Content)
	msg.Content = cleaned
	m.warn(warning)
	switch {
	case m.hostEdit && m.hName.Focused():
		m.hName, cmd = m.hName.Update(msg)
	case m.hostEdit:
		m.hArgs, cmd = m.hArgs.Update(msg)
	case m.section == secName && !m.editing:
		m.name, cmd = m.name.Update(msg)
	}
	return m, cmd
}

func (m formModel) updateHostEdit(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
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
	key = m.typed(key)
	if m.hName.Focused() {
		m.hName, cmd = m.hName.Update(key)
	} else {
		m.hArgs, cmd = m.hArgs.Update(key)
	}
	return m, cmd
}

// typed cleans the text of a key press on its way into a text field. A field
// draws what it holds and nfu then uses it, so nothing goes in uncleaned; a
// terminal can deliver a bidirectional control or an invisible character as
// typed text.
func (m *formModel) typed(key tea.KeyPressMsg) tea.KeyPressMsg {
	if key.Text == "" {
		return key
	}
	cleaned, warning := ui.Typed("The typed text", key.Text)
	key.Text = cleaned
	m.warn(warning)
	return key
}

// warn raises the notice. An empty warning leaves the one that is up.
func (m *formModel) warn(w termsafe.Text) {
	if !w.IsEmpty() {
		m.notice = w
	}
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
	if m.editing {
		name = m.fixed // the file's own name, never what a field made of it
	}
	if err := validateFormName(name); err != "" {
		m.err = err
		return m, nil
	}
	inputs := m.selectedInputNames()
	// A bundle project gets its inputs from `includes`, which this form does not
	// show — so requiring a selection here would make an edit to its hosts or
	// local switch impossible without hand-editing the file.
	if len(inputs) == 0 && len(m.base.Includes) == 0 {
		m.err = "select at least one input (space to toggle)"
		return m, nil
	}

	// Start from the stored definition so description / flakeRoot survive an
	// edit, then overwrite exactly the fields this form owns.
	p := m.base
	p.Inputs = inputs
	p.Hosts = nil
	p.HostDefs = nil
	p.LocalSwitch = nil

	defs := map[string]config.Host{}
	for _, h := range m.hosts {
		if !h.selected {
			continue
		}
		p.Hosts = append(p.Hosts, h.name)
		if len(h.args) > 0 && (!h.known || h.edited) {
			defs[h.name] = config.Host{DeployArgs: h.args}
		}
		// An inline def on the file shadows the shared catalog, so it has to be
		// carried (and refreshed when the args were edited here) rather than
		// left behind to override what we just wrote to the index.
		if old, ok := m.base.HostDefs[h.name]; ok {
			if h.edited {
				old.DeployArgs = h.args
			}
			if p.HostDefs == nil {
				p.HostDefs = map[string]config.Host{}
			}
			p.HostDefs[h.name] = old
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
