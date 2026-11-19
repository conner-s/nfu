package tui

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"forgejo.kube.internal.conn3r.xyz/connerlab/termsafe"

	"github.com/conner-s/nfu/internal/config"
	"github.com/conner-s/nfu/internal/flake"
	"github.com/conner-s/nfu/internal/proc"
	"github.com/conner-s/nfu/internal/project"
	"github.com/conner-s/nfu/internal/ui"
)

// These tests feed the three TUIs text nfu did not write (a flake.lock field,
// a project file, what nix, nh or deploy printed, a file name, what the user
// typed) with terminal control sequences, bidirectional controls and invisible
// characters in it, and check that none of it comes out the other side.
// bubbletea 2.1 replays terminated DCS, APC, PM and SOS strings found in a
// view's text to the terminal, so everything a view draws must already be
// clean. The samples and the two checks are the termsafe module's test kit;
// docs/untrusted-text.md is the table these entries follow.

// entries are the ways outside text gets into the TUIs. Each builds the model
// state a real run would reach with text in that position.
var entries = []struct {
	name  string
	build func(t *testing.T, text string) tea.Model
	// quoted marks a view that draws text an encoder has escaped: see
	// allowed.
	quoted bool
	// pin marks the entry whose text is drawn as a pin, which is clipped to
	// twelve characters: see proof.
	pin bool
	// narrow runs the entry in a terminal 60 columns wide, where the detail
	// view has no room for its TOML pane.
	narrow bool
}{
	// --- the picker (bare `nfu`) ---
	{name: "picker: flake.lock fields", build: func(t *testing.T, text string) tea.Model {
		return loaded(t, lockInputs(text)...)
	}},
	{name: "picker: flake root path", build: func(t *testing.T, text string) tea.Model {
		m := loaded(t, flake.Input{Name: "nixpkgs", Type: "github", Rev: strings.Repeat("a", 40)})
		m.client = flake.New("/home/user/" + text + "/flake")
		return m
	}},
	{name: "picker: upstream pin", pin: true, build: func(t *testing.T, text string) tea.Model {
		m := loaded(t, flake.Input{Name: "nixpkgs", Rev: "old"})
		return step(m, upstreamMsg{name: "nixpkgs", in: flake.Input{Name: "nixpkgs", Rev: text, LastModified: 1_784_000_000}})
	}},
	{name: "picker: upstream probe error", build: func(t *testing.T, text string) tea.Model {
		m := loaded(t, flake.Input{Name: "nixpkgs", Rev: "old"})
		return step(m, upstreamMsg{name: "nixpkgs", err: nixError(text)})
	}},
	// No sample is a name nix can be asked for, so space does not select
	// the row and enter has nothing to update: outside text cannot become the
	// "updating …" line. What is drawn is the list, with the row's note.
	{name: "picker: an input nix cannot be asked for by name", build: func(t *testing.T, text string) tea.Model {
		m := loaded(t, flake.Input{Name: text, Rev: "old"}, flake.Input{Name: "nixpkgs", Rev: "old"})
		m.cursor = 0
		m = step(m, key(" "))
		if len(m.selectedNames()) != 0 {
			t.Fatalf("an input named %q was selected", text)
		}
		return step(m, key("enter"))
	}},
	{name: "picker: update results", build: func(t *testing.T, text string) tea.Model {
		m := loaded(t, flake.Input{Name: text, Rev: "old"})
		results := []flake.UpdateResult{
			{Input: text, OldPin: text, NewPin: "new" + text, OldMod: 1_784_000_000, NewMod: 1_784_100_000},
			{Input: "same" + text, OldPin: text, NewPin: text, Unchanged: true},
		}
		return step(m, finished(results, nil, true, "flake.lock: update "+text, ""))
	}},
	{name: "picker: commit warning", build: func(t *testing.T, text string) tea.Model {
		m := loaded(t, flake.Input{Name: "nixpkgs", Rev: "old"})
		results := []flake.UpdateResult{{Input: "nixpkgs", OldPin: "old", NewPin: "new"}}
		return step(m, finished(results, nil, false, "", "commit failed: git commit -m x -- flake.lock: fatal: "+text))
	}},
	{name: "picker: list after cancel", build: func(t *testing.T, text string) tea.Model {
		m := loaded(t, lockInputs(text)...)
		m.canceled, m.state = true, stateDone
		return m
	}},

	// --- the project manager (`nfu project`) ---
	{name: "manager: project files, list", build: func(t *testing.T, text string) tea.Model {
		return managing(t, hostileConfig(text), mList)
	}},
	// The project's inputs are named text, which is no name nix can update,
	// so the detail view shows the refusal where the steps would be. It
	// quotes the name with %q.
	{name: "manager: project files, detail", quoted: true, narrow: true, build: func(t *testing.T, text string) tea.Model {
		m := managing(t, hostileConfig(text), mDetail)
		m.w = 60 // the TOML pane has its own entry
		return m
	}},
	{name: "manager: steps of a project, detail", narrow: true, build: func(t *testing.T, text string) tea.Model {
		m := managing(t, hostileHosts(text), mDetail)
		m.w = 60
		return m
	}},
	{name: "manager: project files, TOML pane", quoted: true, build: func(t *testing.T, text string) tea.Model {
		return managing(t, hostileConfig(text), mDetail)
	}},
	{name: "manager: a project that will not resolve", quoted: true, narrow: true, build: func(t *testing.T, text string) tea.Model {
		cfg := &config.Config{Projects: map[string]config.Project{
			"lab": {Includes: []string{text}},
		}}
		m := managing(t, cfg, mDetail)
		m.w = 60
		return m
	}},
	{name: "manager: projects directory and current project", build: func(t *testing.T, text string) tea.Model {
		m := managing(t, plainConfig(), mList)
		m.deps.ProjectsDir = "/home/user/" + text + "/projects"
		m.current = text
		return m
	}},
	{name: "manager: run history", build: func(t *testing.T, text string) tea.Model {
		m := managing(t, plainConfig(), mDetail)
		m.runs["lab"] = project.RunRecord{At: time.Now(), OK: true, Detail: "updated 1 · deployed " + text}
		return m
	}},
	{name: "manager: filter text", build: func(t *testing.T, text string) tea.Model {
		m := managing(t, plainConfig(), mList)
		m.filterOn = true
		// A terminal delivers typed text in KeyPressMsg.Text.
		return stepManager(t, m, tea.KeyPressMsg{Code: 'x', Text: text})
	}},
	{name: "manager: plan of a dry run", build: func(t *testing.T, text string) tea.Model {
		m := managing(t, hostileHosts(text), mList)
		next, _ := m.startRun(true)
		if m, ok := next.(managerModel); !ok || m.run == nil {
			t.Fatalf("the dry run did not start")
		}
		return next
	}},
	{name: "manager: a run refused for its input names", quoted: true, build: func(t *testing.T, text string) tea.Model {
		m := managing(t, hostileConfig(text), mList)
		next, _ := m.startRun(true)
		if m, ok := next.(managerModel); !ok || m.run != nil {
			t.Fatalf("a project whose input is named %q got a plan", text)
		}
		return next
	}},
	{name: "manager: nh and deploy output", build: func(t *testing.T, text string) tea.Model {
		m := running(t)
		for _, msg := range childOutput(t, "building "+text, text+"\rsecond state "+text) {
			m = stepManager(t, m, msg)
		}
		return m
	}},
	{name: "manager: error of a failed step", build: func(t *testing.T, text string) tea.Model {
		m := running(t)
		err := nixError(text)
		for _, l := range failLines(err) {
			m = stepManager(t, m, l.at(0))
		}
		return stepManager(t, m, runEndMsg{err: err, detail: "updated 0"})
	}},
	{name: "manager: lock results in the run log", build: func(t *testing.T, text string) tea.Model {
		m := running(t)
		m = stepManager(t, m, runLineMsg{step: 0, tone: toneGood, text: ui.Linef("✓ %s %s → %s", text, ui.Pin(text), ui.Pin("new"+text))})
		return stepManager(t, m, runLineMsg{step: 0, tone: toneWarn, text: ui.Linef("! %s", "commit failed: "+text)})
	}},
	{name: "manager: toast of an error", build: func(t *testing.T, text string) tea.Model {
		m := managing(t, plainConfig(), mList)
		m.sayErr("save: ", fmt.Errorf("open /home/user/%s.toml: permission denied", text))
		return m
	}},
	{name: "manager: toast after a save", build: func(t *testing.T, text string) tea.Model {
		m := managing(t, plainConfig(), mList)
		m.say(ui.Linef("saved ✓ → %s.toml", text), toneGood)
		return m
	}},

	// --- the project form (`nfu project add`, `edit`, and `e`/`n` in the manager) ---
	{name: "form: inputs of the flake", build: func(t *testing.T, text string) tea.Model {
		return newFormModel(lockInputs(text), nil, FormStart{}, ui.NewStyles())
	}},
	{name: "form: stored input the flake no longer has", build: func(t *testing.T, text string) tea.Model {
		start := EditStart("lab", config.Project{Inputs: []string{text}})
		return newFormModel([]flake.Input{{Name: "nixpkgs"}}, nil, start, ui.NewStyles())
	}},
	{name: "form: hosts and their arguments", build: func(t *testing.T, text string) tea.Model {
		known := []HostOption{{Name: "known" + text, Args: []string{"--flag", text}}}
		base := config.Project{
			Inputs:   []string{"nixpkgs"},
			Hosts:    []string{"custom" + text},
			HostDefs: map[string]config.Host{"custom" + text: {DeployArgs: []string{text}}},
		}
		return newFormModel([]flake.Input{{Name: "nixpkgs"}}, known, EditStart("lab", base), ui.NewStyles())
	}},
	{name: "form: name of the project being edited", build: func(t *testing.T, text string) tea.Model {
		return newFormModel([]flake.Input{{Name: "nixpkgs"}}, nil, EditStart(text, config.Project{}), ui.NewStyles())
	}},
	{name: "form: name given on the command line", build: func(t *testing.T, text string) tea.Model {
		return newFormModel([]flake.Input{{Name: "nixpkgs"}}, nil, FormStart{Name: text}, ui.NewStyles())
	}},
	{name: "form: paste into the name", build: func(t *testing.T, text string) tea.Model {
		m := newFormModel([]flake.Input{{Name: "nixpkgs"}}, nil, FormStart{}, ui.NewStyles())
		return formStep(m, tea.PasteMsg{Content: text})
	}},
	{name: "form: typed into the name", build: func(t *testing.T, text string) tea.Model {
		m := newFormModel([]flake.Input{{Name: "nixpkgs"}}, nil, FormStart{}, ui.NewStyles())
		return formStep(m, tea.KeyPressMsg{Code: 'x', Text: text})
	}},
	{name: "form: paste into a host's name and arguments", build: func(t *testing.T, text string) tea.Model {
		m := newFormModel([]flake.Input{{Name: "nixpkgs"}}, nil, FormStart{}, ui.NewStyles())
		m.section = secHosts
		m = formStep(m, key("a"))
		m = formStep(m, tea.PasteMsg{Content: text})
		m = formStep(m, keyTab)
		return formStep(m, tea.PasteMsg{Content: "--flag " + text})
	}},
	{name: "form: stored arguments opened for editing", build: func(t *testing.T, text string) tea.Model {
		known := []HostOption{{Name: "host" + text, Args: []string{"--flag", text}}}
		m := newFormModel([]flake.Input{{Name: "nixpkgs"}}, known, FormStart{}, ui.NewStyles())
		m.section = secHosts
		return formStep(m, key("e"))
	}},
	{name: "form: a host added by hand", build: func(t *testing.T, text string) tea.Model {
		m := newFormModel([]flake.Input{{Name: "nixpkgs"}}, nil, FormStart{}, ui.NewStyles())
		m.section = secHosts
		m = formStep(m, key("a"))
		m = formStep(m, tea.PasteMsg{Content: text})
		return formStep(m, key("enter"))
	}},
}

// lockInputs is a lock whose every field nfu draws holds text.
func lockInputs(text string) []flake.Input {
	return []flake.Input{
		{Name: text, Type: text, Ref: text, Rev: text, LastModified: 1_784_000_000},
		{Name: "hash" + text, Type: "path", NarHash: "sha256-" + text},
		{Name: "alias" + text, Follows: true},
	}
}

// nixError is an error as internal/flake returns one: nix's stderr, whole.
func nixError(text string) error {
	return &flake.CmdError{
		Name:   "nix",
		Err:    errors.New("exit status 1"),
		Stderr: []byte("error: " + text + "\n       second line " + text),
	}
}

// hostileConfig is a configuration whose project and host fields all hold
// text, the way a config.toml or a project file someone else wrote could.
func hostileConfig(text string) *config.Config {
	return &config.Config{
		Hosts: map[string]config.Host{"host" + text: {DeployArgs: []string{"--flag", text}}},
		Projects: map[string]config.Project{
			"lab" + text: {
				Inputs:      []string{text, "second" + text},
				Hosts:       []string{"host" + text},
				Description: "about " + text,
				FlakeRoot:   "/home/user/" + text,
				HostDefs:    map[string]config.Host{"inline" + text: {Hostname: text}},
			},
			"bundle" + text: {Includes: []string{"lab" + text}},
		},
	}
}

// hostileHosts is a project nix can run (its input has an ordinary name)
// whose hosts, deploy arguments, description and flake path hold text.
func hostileHosts(text string) *config.Config {
	return &config.Config{
		Hosts: map[string]config.Host{"host" + text: {DeployArgs: []string{"--flag", text}}},
		Projects: map[string]config.Project{
			"lab": {
				Inputs:      []string{"nixpkgs"},
				Hosts:       []string{"host" + text, text},
				Description: "about " + text,
				FlakeRoot:   "/home/user/" + text,
			},
		},
	}
}

func plainConfig() *config.Config {
	return &config.Config{Projects: map[string]config.Project{
		"lab": {Inputs: []string{"nixpkgs"}, Hosts: []string{"fab"}},
	}}
}

// loaded is the picker after `nix flake metadata` answered with inputs.
func loaded(t *testing.T, inputs ...flake.Input) model {
	t.Helper()
	m := pickingModel()
	m.inputs = nil
	m.state = stateLoading
	m.client = flake.New("/home/user/flake")
	m.spinner = spinner.New(spinner.WithSpinner(spinner.Dot))
	m.now = 1_784_170_000
	return step(m, inputsLoadedMsg{inputs: inputs})
}

// managing is the manager over cfg's projects, showing view.
func managing(t *testing.T, cfg *config.Config, view mview) managerModel {
	t.Helper()
	cat := project.BuildCatalog(cfg, &project.Index{}, nil)
	return managerModel{
		ctx:     context.Background(),
		cancel:  func() {},
		deps:    Deps{Client: flake.New("/home/user/flake"), Catalog: cat, ProjectsDir: "/home/user/projects"},
		g:       ui.NewGruv(),
		spin:    spinner.New(spinner.WithSpinner(spinner.Dot)),
		now:     time.Now().Unix(),
		w:       120,
		h:       40,
		view:    view,
		entries: cat.List(),
		runs:    map[string]project.RunRecord{},
	}
}

// running is the manager with a run of a plain project in flight.
func running(t *testing.T) managerModel {
	t.Helper()
	m := managing(t, plainConfig(), mRun)
	plan := project.Plan{Name: "lab", Inputs: []string{"nixpkgs"}, LocalSwitch: true}
	m.run = &runState{
		name: "lab", title: ui.Name("lab"), plan: plan, steps: stepsForPlan(plan, "/home/user/flake"),
		status: runRunning, started: time.Now(), events: make(chan tea.Msg, 64),
	}
	return m
}

// childOutput runs a child that prints lines and returns the run-log messages
// the engine makes of its output, through the real reader (proc.RunLines).
// The child is cat on a file, because one sample holds a NUL.
func childOutput(t *testing.T, lines ...string) []tea.Msg {
	t.Helper()
	dir := t.TempDir()
	file := filepath.Join(dir, "out")
	if err := os.WriteFile(file, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var msgs []tea.Msg
	err := proc.RunLines(context.Background(), dir, func(l termsafe.Text, rep termsafe.Report) {
		msgs = append(msgs, runLineMsg{step: 0, text: l, tone: toneDim})
		if note := ui.CutNote(rep.Unterminated); !note.IsEmpty() {
			msgs = append(msgs, runLineMsg{step: 0, text: note, tone: toneWarn})
		}
	}, "cat", file)
	if err != nil {
		t.Fatal(err)
	}
	return msgs
}

func stepManager(t *testing.T, m managerModel, msg tea.Msg) managerModel {
	t.Helper()
	next, _ := m.Update(msg)
	got, ok := next.(managerModel)
	if !ok {
		t.Fatalf("Update(%T) returned %T, want managerModel", msg, next)
	}
	return got
}

// Two views draw text that an encoder has already escaped. The TOML pane of
// the detail view draws the project as the TOML encoder writes it, and the
// error for a project that will not resolve quotes a name with %q. Both turn
// an ESC or a C1 control into printable text such as \u001B or \x1b, so a
// control string in a value is drawn as that escape followed by its payload:
// what the value holds, readable and inert. The kit calls a drawn payload a
// problem, because from a cleaner it means half a sequence was removed. For
// these views, and only when the escape is there to be read, it is the
// intended picture; every other finding still fails them.
func allowed(problem, drawn string, quoted bool) bool {
	if problem == "" {
		return true
	}
	if !quoted || !strings.Contains(problem, "the payload of a control sequence") {
		return false
	}
	for _, escape := range []string{`\u001B`, `\x1b`, `\u0090`, `\u009d`} {
		if strings.Contains(drawn, escape) {
			return true
		}
	}
	return false
}

// proof is the text whose presence shows that a sample was drawn at all, so
// that a test cannot pass by drawing nothing. It is the kit's marker, except
// where the sample is drawn as a pin: a pin is clipped to twelve characters,
// and a marker such as <U+FEFF> in front can push the kit's word out of them.
// There the proof is the pin as ui.Pin abbreviates it.
func proof(text string, pin bool) string {
	if pin {
		return ui.Pin(text).String()
	}
	return termsafe.SampleMarker
}

// judged is what the kit's checks are given. They begin by looking for the
// kit's marker; where the proof is something else and has been found, the
// marker is put at the end so that the rest of the check still runs.
func judged(drawn string, pin bool) string {
	if pin && !strings.Contains(drawn, termsafe.SampleMarker) {
		return drawn + termsafe.SampleMarker
	}
	return drawn
}

// TestView_HostileText checks the string every view hands to bubbletea: once
// lipgloss's own colour sequences are set aside, nothing is left in it that
// cleaning would remove, and no payload of a sequence is drawn as text.
func TestView_HostileText(t *testing.T) {
	for _, e := range entries {
		for _, h := range termsafe.Samples() {
			t.Run(e.name+"/"+h.Name, func(t *testing.T) {
				content := e.build(t, h.Text).View().Content
				if want := proof(h.Text, e.pin); want == "" || !strings.Contains(content, want) {
					t.Fatalf("the text is not drawn at all (no %q):\n%q", want, content)
				}
				if p := termsafe.ViewProblem(judged(content, e.pin)); !allowed(p, content, e.quoted) {
					t.Errorf("%s:\n%q", p, content)
				}
			})
		}
	}
}

// still runs a model without its Init: the picker's would start nix.
type still struct{ tea.Model }

func (still) Init() tea.Cmd { return nil }

// syncBuffer stands in for the terminal: the program writes to it from its
// own goroutine while the test reads.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// terminalBytes runs m as a real bubbletea program whose terminal is a buffer,
// waits until want has been drawn, quits, and returns every byte the program
// wrote.
func terminalBytes(t *testing.T, m tea.Model, want string, width int) string {
	t.Helper()
	out := &syncBuffer{}
	p := tea.NewProgram(still{m},
		tea.WithInput(nil),
		tea.WithOutput(out),
		tea.WithoutSignals(),
		tea.WithWindowSize(width, 40),
		tea.WithEnvironment([]string{"TERM=xterm-256color"}),
	)
	done := make(chan error, 1)
	go func() {
		_, err := p.Run()
		done <- err
	}()

	deadline := time.Now().Add(10 * time.Second)
	for !strings.Contains(out.String(), want) {
		if time.Now().After(deadline) {
			p.Kill()
			<-done
			t.Fatalf("the program never drew the text; it wrote:\n%q", out.String())
		}
		time.Sleep(5 * time.Millisecond)
	}
	p.Quit()
	select {
	case err := <-done:
		if err != nil && !errors.Is(err, tea.ErrProgramKilled) {
			t.Fatalf("program: %v", err)
		}
	case <-time.After(10 * time.Second):
		p.Kill()
		t.Fatal("the program did not quit")
	}
	return out.String()
}

// TestProgram_HostileTextNeverReachesTheTerminal is the end-to-end check: the
// text goes in where a real run puts it, the real renderer draws it, and the
// bytes written to the terminal are searched. It is the test that fails when
// the framework passes a sequence through.
func TestProgram_HostileTextNeverReachesTheTerminal(t *testing.T) {
	for _, e := range entries {
		for _, h := range termsafe.Samples() {
			t.Run(e.name+"/"+h.Name, func(t *testing.T) {
				width := 120
				if e.narrow {
					width = 60
				}
				written := terminalBytes(t, e.build(t, h.Text), proof(h.Text, e.pin), width)
				if p := termsafe.TerminalProblem(judged(written, e.pin)); !allowed(p, written, e.quoted) {
					t.Error(p)
				}
			})
		}
	}
}
