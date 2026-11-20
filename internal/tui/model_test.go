package tui

import (
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"github.com/conner-s/termsafe"

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
		upstream:    map[string]probed{},
		upstreamErr: map[string]termsafe.Text{},
		sem:         make(chan struct{}, probeLimit),
	}
}

// key builds the key press the terminal would deliver for s. Text matters as
// much as Code: bubbletea derives String() from Text for printable keys, and
// bubbles inserts Text and nothing else, so a Code-only press types nothing.
func key(s string) tea.KeyPressMsg {
	switch s {
	case " ":
		return tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	default:
		r := []rune(s)
		return tea.KeyPressMsg{Code: r[0], Text: s}
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
	m = step(m, upstreamMsg{name: "nixpkgs", in: flake.Input{Name: "nixpkgs", Rev: "zzz", LastModified: 1784100000}})
	if m.checking["nixpkgs"] {
		t.Error("checking should clear once the result arrives")
	}
	up, ok := m.upstream["nixpkgs"]
	if !ok || up.in.Rev != "zzz" {
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
	m = step(m, upstreamMsg{name: "nixpkgs", err: errTestProbe})
	if m.checking["nixpkgs"] {
		t.Error("checking should clear even on error")
	}
	if m.upstreamErr["nixpkgs"].IsEmpty() {
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
	// F is one probe PER input, so the results land separately — the first row
	// resolves and stops spinning while the second is still in flight.
	m = step(m, upstreamMsg{name: "nixpkgs", in: flake.Input{Name: "nixpkgs", Rev: "new1"}})
	if m.checking["nixpkgs"] {
		t.Error("nixpkgs should clear on its own result")
	}
	if !m.checking["totality"] {
		t.Error("totality should still be in flight — its probe is separate")
	}
	m = step(m, upstreamMsg{name: "totality", in: flake.Input{Name: "totality", Rev: "new2"}})
	if len(m.checking) != 0 {
		t.Errorf("all checking should clear once every probe answers, got %v", m.checking)
	}
	if m.upstream["nixpkgs"].in.Rev != "new1" || m.upstream["totality"].in.Rev != "new2" {
		t.Errorf("both upstream pins should be recorded: %+v", m.upstream)
	}
	if _, ok := m.upstream["mirror"]; ok {
		t.Error("a follows alias is never probed, so it must have no upstream entry")
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
	m = step(m, upstreamMsg{name: "totality", in: flake.Input{Name: "totality", Rev: "t"}})
	if m.checking["totality"] {
		t.Error("totality should clear on its result")
	}
	if !m.checking["nixpkgs"] {
		t.Error("nixpkgs should stay checking until its own f result lands")
	}
	m = step(m, upstreamMsg{name: "nixpkgs", in: flake.Input{Name: "nixpkgs", Rev: "n"}})
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
	m = step(m, upstreamMsg{name: "nixpkgs", in: flake.Input{Name: "nixpkgs", Rev: "z"}})
	if m.now < 1_700_000_000 {
		t.Errorf("upstreamMsg should re-anchor m.now to ~now, got %d", m.now)
	}
}

// ui.ErrLine is what a probe's error becomes in a row and a failure in a
// toast: the first line, trimmed, clipped to 64 characters. It replaced this
// package's shortErr, which ended a clipped line in "…"; a termsafe.Text is
// clipped by Truncate, which ends it in "...".
func TestErrLine(t *testing.T) {
	line := func(s string) string { return ui.ErrLine(errors.New(s)).String() }
	if got := line("  first line \nsecond line"); got != "first line" {
		t.Errorf("multi-line = %q, want %q", got, "first line")
	}
	// A lone carriage return ends a line too: nix redraws with them.
	if got := line("first\rsecond"); got != "first" {
		t.Errorf("carriage return = %q, want %q", got, "first")
	}
	if got := line("nope"); got != "nope" {
		t.Errorf("short = %q, want %q", got, "nope")
	}
	got := line(strings.Repeat("x", 200))
	if utf8.RuneCountInString(got) != 64 || !strings.HasSuffix(got, "...") {
		t.Errorf("capped = %q (%d characters), want 64 ending in ...", got, utf8.RuneCountInString(got))
	}
	// A multibyte line must be cut on a character boundary.
	got = line(strings.Repeat("→", 200))
	if !utf8.ValidString(got) {
		t.Errorf("capped multibyte line is not valid UTF-8: %q", got)
	}
	if utf8.RuneCountInString(got) != 64 {
		t.Errorf("capped multibyte line = %d characters, want 64", utf8.RuneCountInString(got))
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

func TestProbeOrderStartsAtCursorAndWraps(t *testing.T) {
	m := pickingModel() // nixpkgs, totality, mirror(follows)
	m.cursor = 1
	got := strings.Join(m.probeOrder(), " ")
	// Only probeLimit probes run at once, so the hovered row must not queue last.
	if got != "totality nixpkgs" {
		t.Errorf("probeOrder from cursor 1 = %q, want %q", got, "totality nixpkgs")
	}
	m.cursor = 2 // the follows alias: skipped, but the walk still starts there
	if got := strings.Join(m.probeOrder(), " "); got != "nixpkgs totality" {
		t.Errorf("probeOrder from a follows row = %q", got)
	}
}

func TestRefreshKeysProbe(t *testing.T) {
	// `r` re-probes the hovered input; `R` re-probes every real one. Both go
	// through the same path as f/F — the difference is nix's --refresh, which
	// lives in the command, not the model state.
	m := pickingModel()
	next, cmd := m.Update(key("r"))
	m = next.(model)
	if !m.checking["nixpkgs"] || cmd == nil {
		t.Errorf("r should probe the hovered input, checking=%v cmd=%v", m.checking, cmd != nil)
	}

	m = pickingModel()
	next, cmd = m.Update(key("R"))
	m = next.(model)
	if !m.checking["nixpkgs"] || !m.checking["totality"] || cmd == nil {
		t.Errorf("R should probe every real input, checking=%v", m.checking)
	}
	if m.checking["mirror"] {
		t.Error("R must not probe a follows alias")
	}
}

func TestFetchAllIsOneProbePerInput(t *testing.T) {
	// The whole point of the split: F marks each input independently, so an
	// error on one row leaves the others untouched instead of stamping every
	// row with the same failure.
	m := pickingModel()
	m = step(m, key("F"))
	m = step(m, upstreamMsg{name: "nixpkgs", err: errTestProbe})
	if m.upstreamErr["nixpkgs"].IsEmpty() {
		t.Error("the failing input should carry the error")
	}
	if _, ok := m.upstreamErr["totality"]; ok {
		t.Error("one input's probe failure must not mark the others")
	}
	if !m.checking["totality"] {
		t.Error("totality's own probe is still in flight")
	}
}

func TestFetchAllIssuesOneCommandPerInput(t *testing.T) {
	// The structural claim behind the split: `F` returns a BATCH of independent
	// commands — one nix invocation per input — not a single whole-flake probe.
	// Bubbletea runs each on its own goroutine, which is what makes the sweep
	// concurrent and lets rows answer as they land.
	m := pickingModel()
	_, cmd := m.Update(key("F"))
	if cmd == nil {
		t.Fatal("F should return a command")
	}
	batch, ok := cmd().(tea.BatchMsg)
	if !ok {
		t.Fatalf("F should batch several commands, got %T", cmd())
	}
	// 2 real inputs (mirror is a follows alias) + the spinner tick.
	if len(batch) != 3 {
		t.Errorf("F issued %d commands, want 3 (one probe per real input + spinner)", len(batch))
	}

	// A second F while those are in flight adds nothing — no duplicate probes.
	m = step(m, key("F"))
	if _, cmd := m.Update(key("F")); cmd != nil {
		t.Error("F with every input already in flight should issue no further probes")
	}
}

// Every key handler in this package switches on msg.String(). bubbletea v2
// renamed some of those strings (" " became "space"), and the switches compile
// either way, so pin the spellings the arms are written against.
func TestKeyStringsMatchTheSwitchArms(t *testing.T) {
	cases := []struct {
		msg  tea.KeyPressMsg
		want string
	}{
		{tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}, "space"},
		{tea.KeyPressMsg{Code: tea.KeyEnter}, "enter"},
		{tea.KeyPressMsg{Code: tea.KeyEscape}, "esc"},
		{tea.KeyPressMsg{Code: tea.KeyTab}, "tab"},
		{tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}, "shift+tab"},
		{tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl}, "ctrl+s"},
		{tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}, "ctrl+c"},
		{tea.KeyPressMsg{Code: tea.KeyBackspace}, "backspace"},
		{tea.KeyPressMsg{Code: 'G', Text: "G"}, "G"},
	}
	for _, c := range cases {
		if got := c.msg.String(); got != c.want {
			t.Errorf("KeyPressMsg%+v.String() = %q, want %q", c.msg, got, c.want)
		}
	}
}
