package tui

import (
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/conner-s/nfu/internal/flake"
	"github.com/conner-s/nfu/internal/ui"
)

var errTestProbe = errors.New("upstream probe failed: ssh: connect timeout")

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
		selected:    map[int]bool{},
		checking:    map[string]bool{},
		upstream:    map[string]flake.Input{},
		upstreamErr: map[string]string{},
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

func TestFetchHoveredFlow(t *testing.T) {
	m := pickingModel() // cursor 0 = nixpkgs (rev "aaa")
	next, cmd := m.Update(key("f"))
	m = next.(model)
	if !m.checking["nixpkgs"] {
		t.Fatal("f should mark the hovered input as checking")
	}
	if cmd == nil {
		t.Fatal("f should return a fetch command")
	}
	// A result lands with a moved pin.
	m = step(m, upstreamMsg{
		names:  []string{"nixpkgs"},
		inputs: []flake.Input{{Name: "nixpkgs", Rev: "zzz", LastModified: 1784100000}},
	})
	if m.checking["nixpkgs"] {
		t.Error("checking should clear once the result arrives")
	}
	up, ok := m.upstream["nixpkgs"]
	if !ok || up.Rev != "zzz" {
		t.Errorf("upstream pin not recorded: %+v (ok=%v)", up, ok)
	}
	// Only the requested name is recorded, not siblings.
	if _, ok := m.upstream["totality"]; ok {
		t.Error("f on nixpkgs must not record totality's upstream")
	}
}

func TestFetchOnFollowsIsNoop(t *testing.T) {
	m := pickingModel()
	m.cursor = 2 // the follows alias 'mirror'
	next, cmd := m.Update(key("f"))
	m = next.(model)
	if len(m.checking) != 0 {
		t.Errorf("f on a follows alias should be a no-op, got checking=%v", m.checking)
	}
	if cmd != nil {
		t.Error("f on a follows alias should not start a fetch")
	}
}

func TestFetchAllMarksSelectable(t *testing.T) {
	m := pickingModel()
	next, _ := m.Update(key("F"))
	m = next.(model)
	if !m.checking["nixpkgs"] || !m.checking["totality"] {
		t.Errorf("F should mark all real inputs checking, got %v", m.checking)
	}
	if m.checking["mirror"] {
		t.Error("F must not check a follows alias")
	}
}

func TestFetchErrorRecorded(t *testing.T) {
	m := pickingModel()
	m = step(m, key("f")) // marks nixpkgs checking
	m = step(m, upstreamMsg{names: []string{"nixpkgs"}, err: errTestProbe})
	if m.checking["nixpkgs"] {
		t.Error("checking should clear even on error")
	}
	if m.upstreamErr["nixpkgs"] == "" {
		t.Error("a probe error should be recorded for the row")
	}
}

func TestFetchDedupesInFlight(t *testing.T) {
	m := pickingModel()
	m = step(m, key("f")) // nixpkgs now checking
	next, cmd := m.Update(key("f"))
	m = next.(model)
	if cmd != nil {
		t.Error("f on an already-in-flight input should not start a second fetch")
	}
}

func TestFetchAllFlow(t *testing.T) {
	m := pickingModel()
	m = step(m, key("F"))
	// A whole-flake result lands carrying both real inputs (plus the follows
	// alias's resolved pin, which must be ignored).
	m = step(m, upstreamMsg{
		names: []string{"nixpkgs", "totality"},
		inputs: []flake.Input{
			{Name: "nixpkgs", Rev: "new1"},
			{Name: "totality", Rev: "new2"},
			{Name: "mirror", Rev: "new1"},
		},
	})
	if len(m.checking) != 0 {
		t.Errorf("all checking should clear after the F result, got %v", m.checking)
	}
	if m.upstream["nixpkgs"].Rev != "new1" || m.upstream["totality"].Rev != "new2" {
		t.Errorf("both upstream pins should be recorded from the F result: %+v", m.upstream)
	}
	if _, ok := m.upstream["mirror"]; ok {
		t.Error("a follows alias must not get an upstream entry from F")
	}
}

func TestFetchAllWhileHoveredInFlight(t *testing.T) {
	m := pickingModel()
	m = step(m, key("f")) // nixpkgs (cursor 0) in flight
	next, cmd := m.Update(key("F"))
	m = next.(model)
	if !m.checking["nixpkgs"] || !m.checking["totality"] {
		t.Fatalf("F should mark both, got %v", m.checking)
	}
	if cmd == nil {
		t.Fatal("F should still fetch totality (the not-yet-probed input)")
	}
	// totality (from F) resolves first; nixpkgs stays checking until its own result.
	m = step(m, upstreamMsg{names: []string{"totality"}, inputs: []flake.Input{{Name: "totality", Rev: "t"}}})
	if m.checking["totality"] {
		t.Error("totality should clear on its result")
	}
	if !m.checking["nixpkgs"] {
		t.Error("nixpkgs should stay checking until its own f result lands")
	}
	m = step(m, upstreamMsg{names: []string{"nixpkgs"}, inputs: []flake.Input{{Name: "nixpkgs", Rev: "n"}}})
	if m.checking["nixpkgs"] {
		t.Error("nixpkgs should clear after its result")
	}
}

func TestSpinnerTicksWhileProbing(t *testing.T) {
	m := pickingModel()
	m.spinner = spinner.New(spinner.WithSpinner(spinner.Dot))
	// A probe in flight keeps the tick loop alive.
	m.checking["nixpkgs"] = true
	if _, cmd := m.Update(m.spinner.Tick()); cmd == nil {
		t.Fatal("spinner should keep ticking while a probe is in flight")
	}
	// Once checking drains, the loop stops re-scheduling.
	delete(m.checking, "nixpkgs")
	if _, cmd := m.Update(m.spinner.Tick()); cmd != nil {
		t.Fatal("spinner should stop ticking once no probe is in flight")
	}
}

func TestUpstreamMsgReanchorsNow(t *testing.T) {
	m := pickingModel()
	m.now = 1 // clearly stale (1970) — a long-running probe must re-anchor it
	m = step(m, upstreamMsg{names: []string{"nixpkgs"}, inputs: []flake.Input{{Name: "nixpkgs", Rev: "z"}}})
	if m.now < 1_700_000_000 {
		t.Errorf("upstreamMsg should re-anchor m.now to ~now, got %d", m.now)
	}
}

func TestShortErr(t *testing.T) {
	// Multi-line collapses to the first line, trimmed (guards row corruption).
	if got := shortErr(errors.New("  first line \nsecond line")); got != "first line" {
		t.Errorf("multi-line shortErr = %q, want %q", got, "first line")
	}
	// Short single-line returned verbatim.
	if got := shortErr(errors.New("nope")); got != "nope" {
		t.Errorf("short shortErr = %q, want %q", got, "nope")
	}
	// Long ASCII capped to 64 runes ending in an ellipsis.
	got := shortErr(errors.New(strings.Repeat("x", 200)))
	if utf8.RuneCountInString(got) != 64 || !strings.HasSuffix(got, "…") {
		t.Errorf("capped shortErr = %q (%d runes), want 64 runes ending in …", got, utf8.RuneCountInString(got))
	}
	// Multibyte truncation stays valid UTF-8 and rune-counted (not byte-sliced).
	got = shortErr(errors.New(strings.Repeat("→", 200)))
	if !utf8.ValidString(got) {
		t.Errorf("capped multibyte shortErr is not valid UTF-8: %q", got)
	}
	if utf8.RuneCountInString(got) != 64 {
		t.Errorf("capped multibyte shortErr = %d runes, want 64", utf8.RuneCountInString(got))
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
