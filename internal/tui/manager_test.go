package tui

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/conner-s/nfu/internal/config"
	"github.com/conner-s/nfu/internal/flake"
	"github.com/conner-s/nfu/internal/project"
	"github.com/conner-s/nfu/internal/ui"
)

func boolp(v bool) *bool { return &v }

func TestStepsForPlan(t *testing.T) {
	plan := project.Plan{
		Name:        "lab",
		Inputs:      []string{"nixpkgs", "nfu"},
		Commit:      true,
		LocalSwitch: true,
		Hosts: []project.HostStep{
			{Name: "fabricator", Args: []string{"--magic-rollback", "false"}},
			{Name: "edge"},
		},
	}
	steps := stepsForPlan(plan, "/flake")
	verbs := make([]string, len(steps))
	for i, s := range steps {
		verbs[i] = s.verb
	}
	want := []string{"update", "switch", "deploy", "deploy"}
	if strings.Join(verbs, " ") != strings.Join(want, " ") {
		t.Fatalf("verbs = %v, want %v", verbs, want)
	}
	if !strings.Contains(steps[0].cmd, "nix flake update nixpkgs nfu") || !strings.Contains(steps[0].cmd, "commit flake.lock") {
		t.Fatalf("update cmd = %q", steps[0].cmd)
	}
	if steps[1].cmd != "nh os switch /flake" {
		t.Fatalf("switch cmd = %q", steps[1].cmd)
	}
	if steps[2].cmd != "deploy --magic-rollback false .#fabricator" {
		t.Fatalf("deploy cmd = %q", steps[2].cmd)
	}

	// Remote-only, no commit: no switch step, no commit suffix.
	plan.LocalSwitch, plan.Commit = false, false
	steps = stepsForPlan(plan, "/flake")
	if len(steps) != 3 || steps[1].verb != "deploy" {
		t.Fatalf("remote-only steps = %+v", steps)
	}
	if strings.Contains(steps[0].cmd, "commit") {
		t.Fatalf("no-commit cmd should not mention commit: %q", steps[0].cmd)
	}
}

func TestTargetLabelAndStepCount(t *testing.T) {
	cases := []struct {
		p     config.Project
		label string
		count int
	}{
		{config.Project{Inputs: []string{"a"}}, "local", 2},
		{config.Project{Inputs: []string{"a"}, Hosts: []string{"x", "y"}}, "local + ssh:x,y", 4},
		{config.Project{Inputs: []string{"a"}, Hosts: []string{"x"}, LocalSwitch: boolp(false)}, "ssh:x", 2},
		{config.Project{Inputs: []string{"a"}, LocalSwitch: boolp(false)}, "—", 1},
	}
	for _, c := range cases {
		got, _ := targetLabel(c.p)
		if got != c.label {
			t.Errorf("targetLabel(%+v) = %q, want %q", c.p, got, c.label)
		}
		if n := stepCount(c.p); n != c.count {
			t.Errorf("stepCount(%+v) = %d, want %d", c.p, n, c.count)
		}
	}
}

func testManager(names ...string) managerModel {
	entries := make([]project.Entry, 0, len(names))
	for _, n := range names {
		entries = append(entries, project.Entry{
			Name:    n,
			Project: config.Project{Inputs: []string{"nixpkgs"}, Description: "desc " + n},
			Source:  project.SourceAdded,
		})
	}
	return managerModel{
		entries: entries,
		runs:    map[string]project.RunRecord{},
	}
}

func TestFilterMatchesNameAndDescription(t *testing.T) {
	m := testManager("nfu", "npk", "lab")

	m.filter = "np"
	if rows := m.filteredIdx(); len(rows) != 1 || m.entries[rows[0]].Name != "npk" {
		t.Fatalf("filter 'np' rows = %v", rows)
	}

	m.filter = "desc lab"
	if rows := m.filteredIdx(); len(rows) != 1 || m.entries[rows[0]].Name != "lab" {
		t.Fatalf("description filter rows = %v", rows)
	}

	// Selection snaps to the first visible row when the current one is filtered out.
	m.sel = 0
	m.snapSelection()
	if m.entries[m.sel].Name != "lab" {
		t.Fatalf("selection did not snap: %d", m.sel)
	}
}

func TestMoveSelStaysInFilteredRows(t *testing.T) {
	m := testManager("nfu", "npk", "lab")
	m.filter = "n" // nfu, npk (names) — lab matched only if desc contains 'n'; "desc lab" has none
	m.sel = 0
	m.moveSel(1)
	if m.entries[m.sel].Name != "npk" {
		t.Fatalf("moveSel landed on %q", m.entries[m.sel].Name)
	}
	m.moveSel(1) // clamped at the last filtered row
	if m.entries[m.sel].Name != "npk" {
		t.Fatalf("moveSel should clamp, landed on %q", m.entries[m.sel].Name)
	}
}

func TestKeyNavigation(t *testing.T) {
	m := testManager("nfu", "npk", "lab")
	m.g = ui.NewGruv()
	m.deps.Client = flake.New("/flake")
	cfg := &config.Config{Projects: map[string]config.Project{}}
	m.deps.Catalog = project.BuildCatalog(cfg, &project.Index{}, nil)

	press := func(s string) tea.Cmd {
		next, cmd := m.Update(key(s))
		m = next.(managerModel)
		return cmd
	}

	press("j")
	if m.sel != 1 {
		t.Fatalf("j should move selection, sel=%d", m.sel)
	}
	press("enter")
	if m.view != mDetail {
		t.Fatalf("enter should open detail, view=%d", m.view)
	}
	press("l")
	if m.sel != 2 {
		t.Fatalf("l should page to next project, sel=%d", m.sel)
	}
	press("esc")
	if m.view != mList {
		t.Fatalf("esc should return to list, view=%d", m.view)
	}

	// Filter mode: type, narrow, escape clears.
	press("/")
	if !m.filterOn {
		t.Fatal("/ should enter filter mode")
	}
	press("n")
	press("p")
	if m.filter != "np" || m.entries[m.sel].Name != "npk" {
		t.Fatalf("filter=%q sel=%q", m.filter, m.entries[m.sel].Name)
	}
	press("esc")
	if m.filterOn || m.filter != "" {
		t.Fatalf("esc should clear the filter: on=%v %q", m.filterOn, m.filter)
	}

	if cmd := press("q"); cmd == nil || !m.quit {
		t.Fatal("q should quit")
	}
}

func TestRunEventLifecycle(t *testing.T) {
	m := testManager("nfu")
	var recorded *project.RunRecord
	m.deps.Record = func(name string, rec project.RunRecord) error {
		recorded = &rec
		return nil
	}
	plan := project.Plan{Name: "nfu", Inputs: []string{"nixpkgs"}, LocalSwitch: true}
	rs := &runState{
		name: "nfu", plan: plan, steps: stepsForPlan(plan, "/f"),
		status: runRunning, started: time.Now(),
		events: make(chan tea.Msg, 8),
	}
	m.run = rs
	m.view = mRun

	step := func(msg tea.Msg) {
		next, _ := m.Update(msg)
		m = next.(managerModel)
	}
	step(runStepStartMsg{step: 0})
	step(runLineMsg{step: 0, text: "✓ nixpkgs aaaa → bbbb", tone: toneGood})
	step(runStepEndMsg{step: 0, dur: 2 * time.Second})
	if !rs.steps[0].done || rs.steps[0].failed {
		t.Fatalf("step 0 should be done: %+v", rs.steps[0])
	}
	step(runStepStartMsg{step: 1})
	step(runStepEndMsg{step: 1, dur: time.Second, err: errors.New("boom")})
	step(runEndMsg{err: errors.New("boom"), detail: "updated 1"})

	if rs.status != runFail || !rs.steps[1].failed {
		t.Fatalf("run should have failed: status=%d steps=%+v", rs.status, rs.steps)
	}
	if recorded == nil || recorded.OK || recorded.Detail != "updated 1" {
		t.Fatalf("history record = %+v", recorded)
	}
	if got := m.runs["nfu"]; got.OK {
		t.Fatalf("in-memory history should be failed: %+v", got)
	}
}

func TestRunEventSuccessRecordsOK(t *testing.T) {
	m := testManager("nfu")
	plan := project.Plan{Name: "nfu", Inputs: []string{"nixpkgs"}, LocalSwitch: false}
	rs := &runState{
		name: "nfu", plan: plan, steps: stepsForPlan(plan, "/f"),
		status: runRunning, started: time.Now(),
		events: make(chan tea.Msg, 8),
	}
	m.run = rs
	m.view = mRun

	step := func(msg tea.Msg) {
		next, _ := m.Update(msg)
		m = next.(managerModel)
	}
	step(runStepStartMsg{step: 0})
	step(runStepEndMsg{step: 0, dur: time.Second})
	step(runEndMsg{detail: "updated 1"})

	if rs.status != runOK {
		t.Fatalf("status = %d, want runOK", rs.status)
	}
	if got := m.runs["nfu"]; !got.OK || got.Detail != "updated 1" {
		t.Fatalf("history = %+v", got)
	}
}

// TestViewSmoke renders every view once — a panic in layout math (widths,
// slicing, nil entries) fails the test.
func TestViewSmoke(t *testing.T) {
	m := testManager("nfu", "npk")
	m.g = ui.NewGruv()
	m.spin = spinner.New()
	m.deps.Client = flake.New("/flake")
	cfg := &config.Config{Projects: map[string]config.Project{
		"nfu": {Inputs: []string{"nixpkgs"}, Hosts: []string{"fab"}},
	}}
	m.deps.Catalog = project.BuildCatalog(cfg, &project.Index{}, nil)
	m.entries = m.deps.Catalog.List()
	m.runs["nfu"] = project.RunRecord{At: time.Now(), OK: true, Elapsed: 12, Detail: "updated 1"}
	m.w, m.h = 110, 32

	for _, v := range []mview{mList, mDetail, mEditLoading} {
		m.view = v
		if out := m.View(); out == "" {
			t.Fatalf("view %d rendered empty", v)
		}
	}

	// Run view in each status, plus tiny-terminal layouts.
	plan := project.Plan{Name: "nfu", Inputs: []string{"nixpkgs"}, LocalSwitch: true, Hosts: []project.HostStep{{Name: "fab"}}}
	rs := &runState{name: "nfu", plan: plan, steps: stepsForPlan(plan, "/flake"), status: runRunning, started: time.Now(), events: make(chan tea.Msg, 1)}
	rs.lines = append(rs.lines, runLine{step: 0, text: "hello", tone: toneDim})
	m.run = rs
	m.view = mRun
	for _, st := range []runStatus{runRunning, runOK, runFail, runDry} {
		rs.status = st
		if out := m.View(); out == "" {
			t.Fatalf("run view status %d rendered empty", st)
		}
	}
	m.w, m.h = 40, 10
	for _, v := range []mview{mList, mDetail, mRun} {
		m.view = v
		_ = m.View()
	}
}

func TestDryRunPopulatesPlannedLines(t *testing.T) {
	plan := project.Plan{Name: "nfu", Inputs: []string{"nixpkgs"}, LocalSwitch: true}
	steps := stepsForPlan(plan, "/f")
	rs := &runState{name: "nfu", plan: plan, steps: steps, dry: true, status: runDry}
	for i, s := range steps {
		rs.lines = append(rs.lines, runLine{step: i, text: "→ " + s.cmd, tone: toneText})
	}
	if len(rs.lines) != len(steps) {
		t.Fatalf("lines = %d, want %d", len(rs.lines), len(steps))
	}
	if !strings.HasPrefix(rs.lines[0].text, "→ nix flake update") {
		t.Fatalf("first planned line = %q", rs.lines[0].text)
	}
}
