// Package tui is nfu's interactive input picker: run bare `nfu` to see every
// flake input with its age, select the ones to move, and update them in a
// single lock write — the npk-family feel for the update flow.
package tui

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"

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
func Run(ctx context.Context, client *flake.Client, commit bool) error {
	cctx, cancel := context.WithCancel(ctx)
	defer cancel()
	sp := spinner.New(spinner.WithSpinner(spinner.Dot))
	m := model{
		ctx:      cctx,
		cancel:   cancel,
		client:   client,
		commit:   commit,
		styles:   ui.NewStyles(),
		spinner:  sp,
		now:      time.Now().Unix(),
		state:    stateLoading,
		selected: map[int]bool{},
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

	case spinner.TickMsg:
		if m.state == stateLoading || m.state == stateUpdating {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}
		return m, nil

	case tea.KeyMsg:
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

func (m model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		m.cursor = clamp(m.cursor-1, 0, len(m.inputs)-1)
	case "down", "j":
		m.cursor = clamp(m.cursor+1, 0, len(m.inputs)-1)
	case "g", "home":
		m.cursor = 0
	case "G", "end":
		m.cursor = len(m.inputs) - 1
	case " ", "x":
		if m.cursor < len(m.inputs) && !m.inputs[m.cursor].Follows {
			m.selected[m.cursor] = !m.selected[m.cursor]
		}
	case "a":
		m.toggleAll()
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
