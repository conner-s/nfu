package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/conner-s/nfu/internal/flake"
	"github.com/conner-s/nfu/internal/ui"
)

func pickingModel() model {
	return model{
		styles: ui.NewStyles(),
		cancel: func() {},
		state:  statePicking,
		inputs: []flake.Input{
			{Name: "nixpkgs", Rev: "aaa"},
			{Name: "totality", Rev: "bbb"},
			{Name: "mirror", Follows: true},
		},
		selected: map[int]bool{},
	}
}

func key(s string) tea.KeyMsg {
	switch s {
	case " ":
		return tea.KeyMsg{Type: tea.KeySpace}
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
}

func step(m model, msg tea.Msg) model {
	next, _ := m.Update(msg)
	return next.(model)
}

func TestSelectAndNames(t *testing.T) {
	m := pickingModel()
	// space on nixpkgs (cursor 0)
	m = step(m, key(" "))
	if names := m.selectedNames(); len(names) != 1 || names[0] != "nixpkgs" {
		t.Fatalf("after space: %v", names)
	}
	// move to totality and select
	m = step(m, key("down"))
	m = step(m, key(" "))
	if names := m.selectedNames(); len(names) != 2 {
		t.Fatalf("want 2 selected, got %v", names)
	}
	// order follows input order
	if got := m.selectedNames(); got[0] != "nixpkgs" || got[1] != "totality" {
		t.Errorf("selectedNames order = %v", got)
	}
}

func TestToggleAllSkipsFollows(t *testing.T) {
	m := pickingModel()
	m = step(m, key("a"))
	names := m.selectedNames()
	if len(names) != 2 {
		t.Fatalf("toggle-all should select 2 non-follows, got %v", names)
	}
	for _, n := range names {
		if n == "mirror" {
			t.Errorf("follows alias 'mirror' must not be selectable")
		}
	}
	// toggle-all again clears
	m = step(m, key("a"))
	if names := m.selectedNames(); len(names) != 0 {
		t.Errorf("second toggle-all should clear, got %v", names)
	}
}

func TestSpaceOnFollowsIsNoop(t *testing.T) {
	m := pickingModel()
	m.cursor = 2 // the follows alias
	m = step(m, key(" "))
	if names := m.selectedNames(); len(names) != 0 {
		t.Errorf("selecting a follows alias should be a no-op, got %v", names)
	}
}

func TestQuitCancels(t *testing.T) {
	m := pickingModel()
	next, cmd := m.Update(key("q"))
	m = next.(model)
	if !m.canceled || m.state != stateDone {
		t.Errorf("q should cancel and finish: canceled=%v state=%v", m.canceled, m.state)
	}
	if cmd == nil {
		t.Errorf("q should return a quit command")
	}
}

func TestCommitToggle(t *testing.T) {
	// Invoked without -c: the switch starts off and `c` flips it on/off.
	m := pickingModel()
	if m.commit {
		t.Fatal("commit should start off when not invoked with -c")
	}
	m = step(m, key("c"))
	if !m.commit {
		t.Error("c should enable commit")
	}
	m = step(m, key("c"))
	if m.commit {
		t.Error("second c should disable commit")
	}

	// Invoked with -c (or a commit-by-default config): the switch reflects that
	// starting state, and `c` can still turn it off for this run.
	on := pickingModel()
	on.commit = true
	on = step(on, key("c"))
	if on.commit {
		t.Error("c should turn an invoked-on commit off")
	}
}

func TestCursorClamps(t *testing.T) {
	m := pickingModel()
	m = step(m, key("up")) // already at 0
	if m.cursor != 0 {
		t.Errorf("cursor up at top = %d, want 0", m.cursor)
	}
	for i := 0; i < 10; i++ {
		m = step(m, key("down"))
	}
	if m.cursor != len(m.inputs)-1 {
		t.Errorf("cursor down past end = %d, want %d", m.cursor, len(m.inputs)-1)
	}
}

func TestInputsLoadedTransitions(t *testing.T) {
	m := model{styles: ui.NewStyles(), state: stateLoading, selected: map[int]bool{}}
	m = step(m, inputsLoadedMsg{inputs: []flake.Input{{Name: "a"}}})
	if m.state != statePicking {
		t.Errorf("state after load = %v, want picking", m.state)
	}
	// all-follows → nothing to do → done immediately
	m2 := model{styles: ui.NewStyles(), state: stateLoading, selected: map[int]bool{}}
	m2 = step(m2, inputsLoadedMsg{inputs: []flake.Input{{Name: "x", Follows: true}}})
	if m2.state != stateDone {
		t.Errorf("all-follows should finish, state = %v", m2.state)
	}
}
