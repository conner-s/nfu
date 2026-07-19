// The project manager: a full-screen TUI behind bare `nfu project`. It is the
// interactive face of the pipeline verbs — browse projects (list), inspect one
// with its on-disk TOML (detail), run/dry-run the update→switch→deploy pipeline
// with a live log (run), and edit a definition with the same form `add` uses
// (edit). Layout and palette follow the Claude Design prototype (gruvbox).
//
// Like the other TUIs here it stays out of the filesystem: persistence and
// catalog reloads go through the Deps callbacks the CLI wires up.
package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/conner-s/nfu/internal/config"
	"github.com/conner-s/nfu/internal/flake"
	"github.com/conner-s/nfu/internal/project"
	"github.com/conner-s/nfu/internal/ui"
)

// Deps wires the manager to the CLI: the data it browses and the callbacks that
// persist what the user does. All I/O beyond running the pipeline lives behind
// these functions so the model stays testable.
type Deps struct {
	Client      *flake.Client
	Catalog     *project.Catalog
	Current     string
	Runs        map[string]project.RunRecord
	Commit      bool // effective commit-flake.lock decision for runs
	ConfigDir   string
	ProjectsDir string

	Persist    func(name string, p config.Project, defs map[string]config.Host) error
	Reload     func() (*project.Catalog, error)
	SetCurrent func(name string) error
	Record     func(name string, rec project.RunRecord) error
}

type mview int

const (
	mList mview = iota
	mDetail
	mRun
	mEdit
	mEditLoading // fetching flake inputs before the form can open
)

type tone int

const (
	toneDim tone = iota
	toneGood
	toneBad
	toneWarn
	toneText
)

// Messages private to the manager. Run events flow through runState.events and
// are pumped one at a time by waitEvent.
type (
	runStepStartMsg struct{ step int }
	runLineMsg      struct {
		step int
		text string
		tone tone
	}
	runStepEndMsg struct {
		step int
		dur  time.Duration
		err  error
	}
	runEndMsg struct {
		err    error
		detail string
	}
	runTickMsg    struct{}
	sudoDoneMsg   struct{ err error }
	editorDoneMsg struct{ err error }
	toastClearMsg struct{ gen int }
)

type runStatus int

const (
	runRunning runStatus = iota
	runOK
	runFail
	runDry
)

// runStep is one pipeline step as the run view shows it. verb selects the color
// (update/switch/deploy); the engine reports progress by step index.
type runStep struct {
	verb   string
	icon   string
	title  string
	detail string
	cmd    string
	done   bool
	failed bool
	dur    time.Duration
}

type runLine struct {
	step int
	text string
	tone tone
}

// runState is the live (or finished) run the run view renders. Only the update
// loop mutates it; the engine goroutine communicates via events.
type runState struct {
	name        string
	plan        project.Plan
	steps       []runStep
	dry         bool
	no          int
	cur         int
	status      runStatus
	started     time.Time
	elapsed     time.Duration
	lines       []runLine
	events      chan tea.Msg
	canceled    bool
	waitingSudo bool
	detail      string
}

type managerModel struct {
	ctx    context.Context
	cancel context.CancelFunc
	deps   Deps
	g      ui.Gruv
	spin   spinner.Model
	now    int64
	w, h   int

	view     mview
	back     mview // where edit/run return to on exit
	entries  []project.Entry
	runs     map[string]project.RunRecord
	current  string
	sel      int
	top      int // first visible list row (scroll window)
	filterOn bool
	filter   string

	inputs       []flake.Input
	inputsLoaded bool
	editingNew   bool
	form         *formModel

	run       *runState
	runCancel context.CancelFunc
	runNo     int

	toast     string
	toastTone tone
	toastGen  int

	err  error
	quit bool
}

// RunManager opens the project manager. It follows Run's contract: the model
// runs under a cancellable child context (Ctrl-C arrives as a key in raw mode),
// and any fatal error comes back to the caller for the usual `nfu: <err>` line.
func RunManager(ctx context.Context, deps Deps) error {
	cctx, cancel := context.WithCancel(ctx)
	defer cancel()
	m := managerModel{
		ctx:     cctx,
		cancel:  cancel,
		deps:    deps,
		g:       ui.NewGruv(),
		spin:    spinner.New(spinner.WithSpinner(spinner.Dot)),
		now:     time.Now().Unix(),
		view:    mList,
		entries: deps.Catalog.List(),
		runs:    deps.Runs,
		current: deps.Current,
	}
	if m.runs == nil {
		m.runs = map[string]project.RunRecord{}
	}
	final, err := tea.NewProgram(m, tea.WithContext(cctx), tea.WithAltScreen()).Run()
	if err != nil && !errors.Is(err, tea.ErrProgramKilled) && !errors.Is(err, tea.ErrInterrupted) {
		return err
	}
	if ctx.Err() != nil {
		return nil
	}
	if fm, ok := final.(managerModel); ok && fm.err != nil {
		return fm.err
	}
	return nil
}

func (m managerModel) Init() tea.Cmd { return nil }

func (m managerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		return m, nil

	case spinner.TickMsg:
		if m.spinnerLive() {
			var cmd tea.Cmd
			m.spin, cmd = m.spin.Update(msg)
			return m, cmd
		}
		return m, nil

	case toastClearMsg:
		if msg.gen == m.toastGen {
			m.toast = ""
		}
		return m, nil

	case inputsLoadedMsg:
		if m.view != mEditLoading {
			return m, nil
		}
		if msg.err != nil {
			m.view = m.back
			return m, m.say("✗ "+shortErr(msg.err), toneBad)
		}
		m.inputs, m.inputsLoaded = msg.inputs, true
		return m, m.showForm()

	case sudoDoneMsg:
		return m.handleSudoDone(msg)

	case editorDoneMsg:
		if msg.err != nil {
			return m, m.say("✗ editor: "+shortErr(msg.err), toneBad)
		}
		m.reload()
		return m, m.say("reloaded projects", toneDim)

	case runTickMsg:
		if m.run != nil && m.run.status == runRunning && !m.run.waitingSudo {
			m.run.elapsed = time.Since(m.run.started)
			return m, runTick()
		}
		return m, nil

	case runStepStartMsg, runLineMsg, runStepEndMsg, runEndMsg:
		return m.handleRunEvent(msg)

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	if m.view == mEdit {
		return m.delegateForm(msg)
	}
	return m, nil
}

// spinnerLive reports whether anything on screen animates the spinner.
func (m managerModel) spinnerLive() bool {
	if m.view == mEditLoading {
		return true
	}
	return m.run != nil && m.run.status == runRunning
}

func (m managerModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// A running pipeline owns Ctrl-C: it cancels the run, not the program.
	if msg.String() == "ctrl+c" && m.view == mRun && m.run != nil && m.run.status == runRunning {
		return m.cancelRun()
	}
	if msg.String() == "ctrl+c" && m.view != mEdit {
		m.cancel()
		m.quit = true
		return m, tea.Quit
	}

	switch m.view {
	case mList:
		return m.keyList(msg)
	case mDetail:
		return m.keyDetail(msg)
	case mRun:
		return m.keyRun(msg)
	case mEdit:
		return m.delegateForm(msg)
	case mEditLoading:
		if msg.String() == "esc" {
			m.view = m.back
		}
		return m, nil
	}
	return m, nil
}

func (m managerModel) keyList(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.filterOn {
		switch msg.String() {
		case "esc":
			m.filterOn, m.filter = false, ""
		case "enter":
			m.filterOn = false
		case "backspace":
			if len(m.filter) > 0 {
				m.filter = m.filter[:len(m.filter)-1]
			}
			m.snapSelection()
		default:
			if r := msg.Runes; len(r) == 1 {
				m.filter += string(r)
				m.snapSelection()
			}
		}
		return m, nil
	}

	switch msg.String() {
	case "q", "esc":
		m.quit = true
		return m, tea.Quit
	case "down", "j":
		m.moveSel(1)
	case "up", "k":
		m.moveSel(-1)
	case "g", "home":
		if rows := m.filteredIdx(); len(rows) > 0 {
			m.sel = rows[0]
		}
	case "G", "end":
		if rows := m.filteredIdx(); len(rows) > 0 {
			m.sel = rows[len(rows)-1]
		}
	case "enter", "l", "right":
		if m.curEntry() != nil {
			m.view = mDetail
		}
	case "/":
		m.filterOn = true
	case "r":
		return m.startRun(false)
	case "d":
		return m.startRun(true)
	case "e":
		return m.openEdit(false)
	case "n":
		return m.openEdit(true)
	case "E":
		return m.openEditor()
	case "u", ".":
		return m.useCurrent()
	}
	return m, nil
}

func (m managerModel) keyDetail(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "q", "h", "left":
		if msg.String() == "h" || msg.String() == "left" {
			m.cycleSel(-1)
			return m, nil
		}
		m.view = mList
	case "l", "right":
		m.cycleSel(1)
	case "j", "down":
		m.cycleSel(1)
	case "k", "up":
		m.cycleSel(-1)
	case "r":
		return m.startRun(false)
	case "d":
		return m.startRun(true)
	case "e":
		return m.openEdit(false)
	case "E":
		return m.openEditor()
	case "u", ".":
		return m.useCurrent()
	}
	return m, nil
}

func (m managerModel) keyRun(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	r := m.run
	if r == nil {
		m.view = mList
		return m, nil
	}
	if r.status == runRunning {
		if msg.String() == "esc" {
			return m.cancelRun()
		}
		return m, nil
	}
	switch msg.String() {
	case "enter", "esc", "q":
		m.run = nil
		m.view = m.back
	case "r":
		return m.startRun(false)
	case "d":
		return m.startRun(true)
	}
	return m, nil
}

// delegateForm forwards a message to the embedded add/edit form and folds its
// exit (save or discard) back into the manager instead of letting its tea.Quit
// tear the whole program down.
func (m managerModel) delegateForm(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.form == nil {
		m.view = m.back
		return m, nil
	}
	next, cmd := m.form.Update(msg)
	fm, ok := next.(formModel)
	if !ok {
		return m, cmd
	}
	m.form = &fm
	if !fm.quit {
		return m, cmd
	}
	m.form = nil
	m.view = m.back
	if !fm.result.Saved {
		return m, m.say("changes discarded", toneDim)
	}
	if err := m.deps.Persist(fm.result.Name, fm.result.Project, fm.result.HostDefs); err != nil {
		return m, m.say("✗ save: "+shortErr(err), toneBad)
	}
	m.reload()
	m.selectName(fm.result.Name)
	return m, m.say("saved ✓ → "+fm.result.Name+".toml", toneGood)
}

// --- selection / filtering -------------------------------------------------

// filteredIdx returns the entry indices matching the filter, in display order.
func (m managerModel) filteredIdx() []int {
	f := strings.ToLower(m.filter)
	var out []int
	for i, e := range m.entries {
		if f == "" || strings.Contains(strings.ToLower(e.Name), f) ||
			strings.Contains(strings.ToLower(e.Project.Description), f) {
			out = append(out, i)
		}
	}
	return out
}

func (m *managerModel) moveSel(d int) {
	rows := m.filteredIdx()
	if len(rows) == 0 {
		return
	}
	pos := 0
	for i, idx := range rows {
		if idx == m.sel {
			pos = i
			break
		}
	}
	pos = clamp(pos+d, 0, len(rows)-1)
	m.sel = rows[pos]
}

// cycleSel wraps around the filtered rows (the detail view's h/l paging).
func (m *managerModel) cycleSel(d int) {
	rows := m.filteredIdx()
	if len(rows) == 0 {
		return
	}
	pos := 0
	for i, idx := range rows {
		if idx == m.sel {
			pos = i
			break
		}
	}
	m.sel = rows[(pos+d+len(rows))%len(rows)]
}

// snapSelection keeps the selection on a visible row while the filter narrows.
func (m *managerModel) snapSelection() {
	rows := m.filteredIdx()
	if len(rows) == 0 {
		return
	}
	for _, idx := range rows {
		if idx == m.sel {
			return
		}
	}
	m.sel = rows[0]
}

func (m managerModel) curEntry() *project.Entry {
	if m.sel < 0 || m.sel >= len(m.entries) {
		return nil
	}
	e := m.entries[m.sel]
	return &e
}

func (m *managerModel) selectName(name string) {
	for i, e := range m.entries {
		if e.Name == name {
			m.sel = i
			return
		}
	}
}

// reload rebuilds the catalog view after a save or external edit.
func (m *managerModel) reload() {
	if m.deps.Reload == nil {
		return
	}
	cat, err := m.deps.Reload()
	if err != nil {
		return // keep the stale view; the next action will surface the error
	}
	m.deps.Catalog = cat
	m.entries = cat.List()
	if m.sel >= len(m.entries) {
		m.sel = clamp(len(m.entries)-1, 0, len(m.entries))
	}
}

// --- actions ---------------------------------------------------------------

func (m managerModel) useCurrent() (tea.Model, tea.Cmd) {
	e := m.curEntry()
	if e == nil {
		return m, nil
	}
	if m.deps.SetCurrent != nil {
		if err := m.deps.SetCurrent(e.Name); err != nil {
			return m, m.say("✗ "+shortErr(err), toneBad)
		}
	}
	m.current = e.Name
	return m, m.say("current project: "+e.Name, toneGood)
}

// openEdit opens the embedded form for the selected project (or a blank one).
// The form needs the flake's inputs, which is a nix call — fetched once, with a
// loading state, then cached for the session.
func (m managerModel) openEdit(newProject bool) (tea.Model, tea.Cmd) {
	if !newProject {
		e := m.curEntry()
		if e == nil {
			return m, nil
		}
		if e.Source == project.SourceDeclared {
			return m, m.say("read-only (declared) — edit config.toml / home-manager", toneWarn)
		}
	}
	m.editingNew = newProject
	m.back = m.view
	if !m.inputsLoaded {
		m.view = mEditLoading
		client, ctx := m.deps.Client, m.ctx
		return m, tea.Batch(m.spin.Tick, func() tea.Msg {
			inputs, err := client.Inputs(ctx)
			return inputsLoadedMsg{inputs: inputs, err: err}
		})
	}
	return m, m.showForm()
}

func (m *managerModel) showForm() tea.Cmd {
	var known []HostOption
	for hn, h := range m.deps.Catalog.Hosts() {
		known = append(known, HostOption{Name: hn, Args: h.DeployArgs})
	}
	start := FormStart{}
	if !m.editingNew {
		if e := m.curEntry(); e != nil {
			start = FormStart{
				Name: e.Name, Inputs: e.Project.Inputs, Hosts: e.Project.Hosts,
				LocalSwitch: e.Project.SwitchLocal(), Editing: true,
			}
		}
	}
	fm := newFormModel(m.inputs, known, start)
	m.form = &fm
	m.view = mEdit
	return textinput.Blink
}

// openEditor releases the terminal to $EDITOR on the project's TOML file, then
// reloads the catalog. Declared-in-config projects have no file to open.
func (m managerModel) openEditor() (tea.Model, tea.Cmd) {
	e := m.curEntry()
	if e == nil {
		return m, nil
	}
	path := filepath.Join(m.deps.ProjectsDir, e.Name+".toml")
	if _, err := os.Stat(path); err != nil {
		return m, m.say("no file — declared in config.toml", toneWarn)
	}
	ed := strings.Fields(os.Getenv("EDITOR"))
	if len(ed) == 0 {
		return m, m.say("$EDITOR is not set", toneWarn)
	}
	c := exec.Command(ed[0], append(ed[1:], path)...)
	return m, tea.ExecProcess(c, func(err error) tea.Msg { return editorDoneMsg{err} })
}

// --- toast -----------------------------------------------------------------

// say shows a transient footer message (the prototype's toast).
func (m *managerModel) say(text string, t tone) tea.Cmd {
	m.toast, m.toastTone = text, t
	m.toastGen++
	gen := m.toastGen
	return tea.Tick(2600*time.Millisecond, func(time.Time) tea.Msg { return toastClearMsg{gen} })
}

func (m managerModel) toneStyle(t tone) func(...string) string {
	switch t {
	case toneGood:
		return m.g.Green.Render
	case toneBad:
		return m.g.Red.Render
	case toneWarn:
		return m.g.Yellow.Render
	case toneText:
		return m.g.Fg.Render
	default:
		return m.g.Gray.Render
	}
}

// --- misc helpers ----------------------------------------------------------

// targetLabel summarizes where a project lands: the local host, ssh deploys, or
// both. The second return picks the display tone (aqua local, blue remote).
func targetLabel(p config.Project) (string, tone) {
	local := p.SwitchLocal()
	switch {
	case local && len(p.Hosts) > 0:
		return "local + ssh:" + strings.Join(p.Hosts, ","), toneGood
	case local:
		return "local", toneGood
	case len(p.Hosts) > 0:
		return "ssh:" + strings.Join(p.Hosts, ","), toneText
	default:
		return "—", toneDim
	}
}

// stepCount mirrors stepsForPlan without needing a resolved plan.
func stepCount(p config.Project) int {
	n := 1 // update
	if p.SwitchLocal() {
		n++
	}
	return n + len(p.Hosts)
}

func fmtDur(d time.Duration) string {
	d = d.Round(time.Second)
	if d >= time.Minute {
		return fmt.Sprintf("%d:%02d", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%ds", int(d.Seconds()))
}
