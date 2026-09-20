// Package tui is nfu's interactive input picker: run bare `nfu` to see every
// flake input with its age, select the ones to move, and update them in a
// single lock write — the npk-family feel for the update flow.
package tui

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"

	"github.com/conner-s/nfu/internal/flake"
	"github.com/conner-s/nfu/internal/lock"
	"github.com/conner-s/nfu/internal/ui"
)

type state int

const (
	stateLoading state = iota
	statePicking
	stateUpdating
	stateDone
)

type inputsLoadedMsg struct {
	inputs []flake.Input
	err    error
}

type updateFinishedMsg struct {
	results   []flake.UpdateResult
	err       error
	committed bool
	commitMsg string
	warn      string
}

// upstreamMsg carries the result of ONE input's upstream probe. Every probe is
// its own nix invocation and its own message, so a row updates the moment its
// own answer lands rather than waiting on the slowest input in the sweep, and a
// remote that is down marks only the rows that actually depend on it.
type upstreamMsg struct {
	name string
	in   flake.Input
	err  error
}

// probeLimit caps how many upstream probes run at once. Each is a short-lived
// nix process (~40ms of startup against ~250ms of network), so the cap is about
// the remotes, not the CPU: it keeps a wide flake from opening two dozen
// simultaneous connections to the same few forges.
const probeLimit = 8

type model struct {
	ctx     context.Context
	cancel  context.CancelFunc
	client  *flake.Client
	commit  bool
	styles  ui.Styles
	spinner spinner.Model
	now     int64

	state    state
	inputs   []flake.Input
	cursor   int
	selected map[int]bool
	updating []string

	// Upstream-probe state (the `f`/`F`/`r`/`R` hotkeys). checking holds inputs
	// with an in-flight probe (drives the per-row spinner); upstream holds the
	// resolved upstream pin by input name; upstreamErr holds a probe's error by
	// name. All keyed by input name so a re-fetch simply overwrites. sem bounds
	// how many probes talk to nix at once (see probeLimit).
	checking    map[string]bool
	upstream    map[string]flake.Input
	upstreamErr map[string]string
	sem         chan struct{}

	results   []flake.UpdateResult
	committed bool
	commitMsg string
	warn      string
	err       error
	canceled  bool
}

// Run opens the picker against client, committing flake.lock afterward when
// commit is set. It returns any fatal error (a failed load or update).
//
// The model runs under a child context it can cancel: because bubbletea holds
// the tty in raw mode, Ctrl-C arrives as a key event (not SIGINT), so aborting a
// slow `nix flake metadata`/`update` requires cancelling that context from the
// key handler. tea.WithContext also tears the program down on an external signal.
func Run(ctx context.Context, client *flake.Client, commit bool, s ui.Styles) error {
	cctx, cancel := context.WithCancel(ctx)
	defer cancel()
	sp := spinner.New(spinner.WithSpinner(spinner.Dot))
	m := model{
		ctx:         cctx,
		cancel:      cancel,
		client:      client,
		commit:      commit,
		styles:      s,
		spinner:     sp,
		now:         time.Now().Unix(),
		state:       stateLoading,
		selected:    map[int]bool{},
		checking:    map[string]bool{},
		upstream:    map[string]flake.Input{},
		upstreamErr: map[string]string{},
		sem:         make(chan struct{}, probeLimit),
	}
	final, err := tea.NewProgram(m, tea.WithContext(cctx)).Run()
	if err != nil && !errors.Is(err, tea.ErrProgramKilled) && !errors.Is(err, tea.ErrInterrupted) {
		return err
	}
	// An external signal that cancelled ctx is a clean, user-initiated stop.
	if ctx.Err() != nil {
		return nil
	}
	if fm, ok := final.(model); ok && fm.err != nil {
		return fm.err
	}
	return nil
}

func (m model) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, m.loadInputs())
}

func (m model) loadInputs() tea.Cmd {
	return func() tea.Msg {
		inputs, err := m.client.Inputs(m.ctx)
		return inputsLoadedMsg{inputs: inputs, err: err}
	}
}

func (m model) doUpdate(names []string) tea.Cmd {
	return func() tea.Msg {
		results, err := m.client.Update(m.ctx, names...)
		var committed bool
		var commitMsg, warn string
		if results != nil {
			committed, commitMsg, warn = lock.Commit(m.ctx, m.client.Root(), results, m.commit)
		}
		return updateFinishedMsg{results: results, err: err, committed: committed, commitMsg: commitMsg, warn: warn}
	}
}

// fetchUpstream probes one input in the background and reports it under its own
// name. Bubbletea runs each Cmd on its own goroutine, so a batch of these is
// genuinely concurrent; m.sem bounds how many reach nix at once, and a probe
// blocked on the semaphore holds nothing but a goroutine (its row already shows
// "checking…", which is honest either way).
func (m model) fetchUpstream(name string, refresh bool) tea.Cmd {
	client, ctx, sem := m.client, m.ctx, m.sem
	return func() tea.Msg {
		select {
		case sem <- struct{}{}:
			defer func() { <-sem }()
		case <-ctx.Done():
			return upstreamMsg{name: name, err: ctx.Err()}
		}
		in, err := client.Upstream(ctx, name, refresh)
		return upstreamMsg{name: name, in: in, err: err}
	}
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case inputsLoadedMsg:
		if msg.err != nil {
			m.err = msg.err
			m.state = stateDone
			return m, tea.Quit
		}
		m.inputs = msg.inputs
		m.cursor = firstSelectable(m.inputs)
		m.state = statePicking
		if !anySelectable(m.inputs) {
			// Nothing to update (all follows / empty): show the list and exit.
			// canceled routes doneView to the "list + (no changes)" frame.
			m.canceled = true
			m.state = stateDone
			return m, tea.Quit
		}
		return m, nil

	case updateFinishedMsg:
		m.results = msg.results
		m.err = msg.err
		m.committed = msg.committed
		m.commitMsg = msg.commitMsg
		m.warn = msg.warn
		m.state = stateDone
		return m, tea.Quit

	case upstreamMsg:
		// A probe just resolved: re-anchor now so RelAge's second-precision reads
		// true against the moment the result landed, then clear this input's
		// in-flight mark. Probes land one at a time, so the row updates while its
		// neighbours are still spinning.
		m.now = time.Now().Unix()
		delete(m.checking, msg.name)
		if msg.err != nil {
			m.upstreamErr[msg.name] = shortErr(msg.err)
			return m, nil
		}
		m.upstream[msg.name] = msg.in
		delete(m.upstreamErr, msg.name)
		return m, nil

	case spinner.TickMsg:
		// Keep the spinner alive while loading, updating, or any upstream probe is
		// in flight (the per-row "checking…" indicator).
		if m.state == stateLoading || m.state == stateUpdating || len(m.checking) > 0 {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}
		return m, nil

	case tea.KeyPressMsg:
		// Quit keys work in every state: during loading/updating they cancel the
		// in-flight nix subprocess (raw mode means Ctrl-C is a key, not a signal).
		switch msg.String() {
		case "ctrl+c", "q", "esc":
			m.cancel()
			m.canceled = true
			m.state = stateDone
			return m, tea.Quit
		}
		if m.state == statePicking {
			return m.handleKey(msg)
		}
		return m, nil
	}
	return m, nil
}

func (m model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		m.cursor = clamp(m.cursor-1, 0, len(m.inputs)-1)
	case "down", "j":
		m.cursor = clamp(m.cursor+1, 0, len(m.inputs)-1)
	case "g", "home":
		m.cursor = 0
	case "G", "end":
		m.cursor = len(m.inputs) - 1
	case "space", "x":
		if m.cursor < len(m.inputs) && !m.inputs[m.cursor].Follows {
			m.selected[m.cursor] = !m.selected[m.cursor]
		}
	case "a":
		m.toggleAll()
	case "f":
		// Probe upstream for the hovered input only.
		return m.startFetch(false, false)
	case "F":
		// Probe upstream for every real input, concurrently.
		return m.startFetch(true, false)
	case "r":
		// Same probe, ignoring nix's caches — for a ref that moved under nix
		// (a force-push), where the ordinary probe answers from what it holds.
		return m.startFetch(false, true)
	case "R":
		return m.startFetch(true, true)
	case "c":
		// Flip whether a successful update also commits flake.lock. Starts from
		// the invoked state (-c / --no-commit / config default) and overrides it
		// for this run only.
		m.commit = !m.commit
	case "enter", "u":
		names := m.selectedNames()
		if len(names) == 0 {
			m.canceled = true
			m.state = stateDone
			return m, tea.Quit
		}
		m.updating = names
		m.state = stateUpdating
		return m, tea.Batch(m.spinner.Tick, m.doUpdate(names))
	}
	return m, nil
}

// startFetch kicks background upstream probes, ONE nix invocation per input, up
// to probeLimit at a time. `f`/`r` (all==false) check just the hovered input;
// `F`/`R` (all==true) check every real input; the `r` pair passes --refresh. The
// picker stays live throughout — each checked row spins until its own answer
// lands, and answers land independently rather than all at the end.
//
// Inputs already in flight are not re-probed (so `F` after an `f` covers only the
// remaining ones), and the spinner tick loop is started only when one wasn't
// already running (any probe keeps it alive).
func (m model) startFetch(all, refresh bool) (tea.Model, tea.Cmd) {
	wasChecking := len(m.checking) > 0
	var targets []string
	if all {
		targets = m.probeOrder()
	} else {
		if m.cursor >= len(m.inputs) {
			return m, nil
		}
		in := m.inputs[m.cursor]
		if in.Follows {
			// A `follows` alias has no independent lock entry to probe.
			return m, nil
		}
		targets = []string{in.Name}
	}

	var cmds []tea.Cmd
	for _, n := range targets {
		if m.checking[n] {
			continue
		}
		m.checking[n] = true
		cmds = append(cmds, m.fetchUpstream(n, refresh))
	}
	if len(cmds) == 0 {
		return m, nil // everything requested is already being checked
	}
	if !wasChecking {
		cmds = append(cmds, m.spinner.Tick)
	}
	return m, tea.Batch(cmds...)
}

func (m *model) toggleAll() {
	// If every selectable input is already selected, clear; otherwise select all.
	allOn := true
	for i, in := range m.inputs {
		if in.Follows {
			continue
		}
		if !m.selected[i] {
			allOn = false
			break
		}
	}
	for i, in := range m.inputs {
		if in.Follows {
			continue
		}
		m.selected[i] = !allOn
	}
}

func (m model) selectedNames() []string {
	var names []string
	for i, in := range m.inputs {
		if in.Follows {
			continue
		}
		if m.selected[i] {
			names = append(names, in.Name)
		}
	}
	return names
}

// probeOrder returns every real (non-follows) input name — the set a whole-flake
// probe answers for — starting at the cursor and wrapping. Only probeLimit run
// at once, so the order decides which rows answer FIRST, and the row the user is
// looking at should not be last in line behind twenty others.
func (m model) probeOrder() []string {
	names := make([]string, 0, len(m.inputs))
	for off := range m.inputs {
		in := m.inputs[(m.cursor+off)%len(m.inputs)]
		if !in.Follows {
			names = append(names, in.Name)
		}
	}
	return names
}

// shortErr condenses a probe error to a single trimmed, length-capped line for
// an inline row marker — the full multi-line nix stderr would overflow the table.
func shortErr(err error) string {
	s := err.Error()
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	s = strings.TrimSpace(s)
	const maxLen = 64
	if utf8.RuneCountInString(s) > maxLen {
		s = string([]rune(s)[:maxLen-1]) + "…"
	}
	return s
}

func firstSelectable(inputs []flake.Input) int {
	for i, in := range inputs {
		if !in.Follows {
			return i
		}
	}
	return 0
}

func anySelectable(inputs []flake.Input) bool {
	for _, in := range inputs {
		if !in.Follows {
			return true
		}
	}
	return false
}

func clamp(v, lo, hi int) int {
	if hi < lo {
		return lo
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// buildUpdatingLabel is used by the view; kept here for locality with updating.
func (m model) buildUpdatingLabel() string {
	return strings.Join(m.updating, " ")
}
