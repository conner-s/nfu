// The manager's run engine: executes a resolved plan (update → nh os switch →
// deploy…) in a goroutine, streaming step starts, log lines, and step results
// back to the model as messages. It mirrors project.Run's orchestration but
// captures child output for the run view instead of inheriting the terminal.
package tui

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/conner-s/termsafe"

	"github.com/conner-s/nfu/internal/flake"
	"github.com/conner-s/nfu/internal/lock"
	"github.com/conner-s/nfu/internal/proc"
	"github.com/conner-s/nfu/internal/project"
	"github.com/conner-s/nfu/internal/ui"
)

// stepsForPlan lays a plan out as the run view's step list. Ordering must match
// the engine's emission order: update, then switch (when local), then each host.
// The plan's inputs, hosts and arguments come from project files, and root from
// the configuration; they are cleaned here, where they become text to draw.
func stepsForPlan(plan project.Plan, root string) []runStep {
	commit := ""
	if plan.Commit {
		commit = " + commit flake.lock"
	}
	steps := []runStep{{
		verb:   "update",
		icon:   "↻",
		title:  ui.Words("update inputs"),
		detail: ui.Names(plan.Inputs, ", "),
		cmd:    ui.Linef("nix flake update %s%s", ui.Names(plan.Inputs, " "), commit),
	}}
	if plan.LocalSwitch {
		steps = append(steps, runStep{
			verb:   "switch",
			icon:   "❄",
			title:  ui.Words("nh os switch"),
			detail: ui.Line(root),
			cmd:    ui.Linef("nh os switch %s", root),
		})
	}
	for _, h := range plan.Hosts {
		argv := ui.Names(h.Argv(), " ")
		steps = append(steps, runStep{
			verb:   "deploy",
			icon:   "➤",
			title:  ui.Linef("deploy %s", h.Name),
			detail: argv,
			cmd:    ui.Linef("deploy %s", argv),
		})
	}
	return steps
}

// startRun builds the plan for the selected project and either renders it as a
// dry-run or launches the engine. A plan with a local switch first pre-caches
// sudo credentials outside the raw-mode terminal (see handleSudoDone) so nh's
// own sudo never has to prompt into a pipe.
func (m managerModel) startRun(dry bool) (tea.Model, tea.Cmd) {
	e := m.curEntry()
	if e == nil {
		return m, nil
	}
	if m.run != nil && m.run.status == runRunning {
		return m, m.say(ui.Words("a run is already in flight"), toneWarn)
	}
	plan, err := project.BuildPlan(m.deps.Catalog, e.Name, m.deps.Commit)
	if err != nil {
		return m, m.sayErr("", err)
	}
	root := m.deps.Client.Root()
	steps := stepsForPlan(plan, root)
	if m.view != mRun {
		m.back = m.view
	}
	m.runNo++

	if dry {
		rs := &runState{name: e.Name, title: ui.Name(e.Name), plan: plan, steps: steps, dry: true, no: m.runNo, status: runDry, cur: len(steps) - 1}
		for i, s := range steps {
			rs.lines = append(rs.lines, runLine{step: i, text: ui.Linef("→ %s", s.cmd), tone: toneText})
		}
		m.run = rs
		m.view = mRun
		return m, m.say(ui.Words("dry-run · nothing executed"), toneWarn)
	}

	runCtx, cancel := context.WithCancel(m.ctx)
	m.runCancel = cancel
	rs := &runState{
		name: e.Name, title: ui.Name(e.Name), plan: plan, steps: steps, no: m.runNo,
		status: runRunning, started: time.Now(),
		events: make(chan tea.Msg, 64),
	}
	m.run = rs
	m.view = mRun

	// Pre-authorize sudo for the local switch: tea.ExecProcess hands the real
	// terminal to `sudo -v`, so a password prompt works; with cached credentials
	// it returns instantly. Remote-only plans skip straight to the engine.
	// What sudo writes while it has the terminal is not cleaned.
	if plan.LocalSwitch {
		if sudo, err := exec.LookPath("sudo"); err == nil {
			rs.waitingSudo = true
			return m, tea.ExecProcess(exec.Command(sudo, "-v"), func(err error) tea.Msg { return sudoDoneMsg{err} })
		}
	}
	return m, m.launchEngine(runCtx, rs)
}

func (m managerModel) handleSudoDone(msg sudoDoneMsg) (tea.Model, tea.Cmd) {
	rs := m.run
	if rs == nil || !rs.waitingSudo {
		return m, nil
	}
	rs.waitingSudo = false
	if msg.err != nil {
		rs.status = runFail
		rs.lines = append(rs.lines, runLine{step: rs.cur, text: ui.Words("sudo authorization failed — run aborted"), tone: toneBad})
		if m.runCancel != nil {
			m.runCancel()
		}
		return m, m.say(ui.Words("✗ sudo authorization failed"), toneBad)
	}
	rs.started = time.Now() // don't count the password prompt as pipeline time
	runCtx, cancel := context.WithCancel(m.ctx)
	m.runCancel = cancel
	return m, m.launchEngine(runCtx, rs)
}

// launchEngine starts the pipeline goroutine and the message loops that animate
// the view: the event pump, the elapsed ticker, and the spinner.
func (m *managerModel) launchEngine(ctx context.Context, rs *runState) tea.Cmd {
	go runEngine(ctx, m.deps.Client, rs.plan, rs.events)
	return tea.Batch(waitEvent(rs.events), runTick(), m.spin.Tick)
}

func (m managerModel) cancelRun() (tea.Model, tea.Cmd) {
	if m.runCancel != nil {
		m.runCancel()
	}
	if m.run != nil {
		m.run.canceled = true
	}
	// The engine notices the dead context, fails its current step, and emits
	// runEndMsg — the pump stays alive until then.
	return m, m.say(ui.Words("cancelling…"), toneWarn)
}

// waitEvent pumps one engine event into the update loop; the handler re-arms it
// until runEndMsg lands.
func waitEvent(ch chan tea.Msg) tea.Cmd {
	return func() tea.Msg { return <-ch }
}

func runTick() tea.Cmd {
	return tea.Tick(500*time.Millisecond, func(time.Time) tea.Msg { return runTickMsg{} })
}

// handleRunEvent applies one engine event to the run state.
func (m managerModel) handleRunEvent(msg tea.Msg) (tea.Model, tea.Cmd) {
	rs := m.run
	if rs == nil {
		return m, nil
	}
	switch ev := msg.(type) {
	case runStepStartMsg:
		rs.cur = ev.step
	case runLineMsg:
		rs.lines = append(rs.lines, runLine(ev))
	case runStepEndMsg:
		if ev.step < len(rs.steps) {
			s := &rs.steps[ev.step]
			s.dur = ev.dur
			s.done = ev.err == nil
			s.failed = ev.err != nil
		}
	case runEndMsg:
		rs.elapsed = time.Since(rs.started)
		rs.detail = ev.detail
		if ev.err != nil {
			rs.status = runFail
			if rs.canceled {
				rs.detail = "cancelled"
			}
		} else {
			rs.status = runOK
		}
		return m, m.finishRun(rs, ev.err)
	}
	return m, waitEvent(rs.events)
}

// finishRun records history and refreshes the list row after a real run.
func (m *managerModel) finishRun(rs *runState, err error) tea.Cmd {
	rec := project.RunRecord{
		At:      time.Now(),
		OK:      err == nil,
		Elapsed: int64(rs.elapsed.Seconds()),
		Detail:  rs.detail,
	}
	m.runs[rs.name] = rec
	if m.deps.Record != nil {
		_ = m.deps.Record(rs.plan, rec) // history is best-effort, never fatal
	}
	if err != nil {
		return m.sayErr("", err)
	}
	return m.say(ui.Linef("✓ %d steps complete", len(rs.steps)), toneGood)
}

// runEngine executes the pipeline, emitting progress events. Step indices match
// stepsForPlan: 0 update, then switch (when local), then one per host. It owns
// no model state — everything flows through ch, ending with exactly one
// runEndMsg.
//
// This is where the run pane's log is written, so it is where that text is
// cleaned: say takes a termsafe.Text, built by ui.Linef from a result or a
// warning, by ui.ErrLines from an error, or by proc.RunLines from a line of nh
// or deploy.
func runEngine(ctx context.Context, client *flake.Client, plan project.Plan, ch chan<- tea.Msg) {
	emit := func(msg tea.Msg) { ch <- msg }
	say := func(step int, t tone, text termsafe.Text) {
		emit(runLineMsg{step: step, text: text, tone: t})
	}
	// logLine takes one line of a child's output. A control string without a
	// terminator took the rest of its line with it; say so under that line.
	logLine := func(step int) func(termsafe.Text, termsafe.Report) {
		return func(l termsafe.Text, rep termsafe.Report) {
			say(step, toneDim, l)
			if note := ui.CutNote(rep.Unterminated); !note.IsEmpty() {
				say(step, toneWarn, note)
			}
		}
	}
	var (
		moved    int
		switched bool
		deployed []string
	)
	detail := func() string {
		parts := []string{fmt.Sprintf("updated %d", moved)}
		if switched {
			parts = append(parts, "switched")
		}
		if len(deployed) > 0 {
			parts = append(parts, "deployed "+strings.Join(deployed, ", "))
		}
		return strings.Join(parts, " · ")
	}
	fail := func(step int, t0 time.Time, err error) {
		emit(runStepEndMsg{step: step, dur: time.Since(t0), err: err})
		for _, l := range failLines(err) {
			emit(l.at(step))
		}
		emit(runEndMsg{err: err, detail: detail()})
	}

	// Step 0: update the inputs in one lock write, then commit flake.lock.
	step := 0
	emit(runStepStartMsg{step: step})
	t0 := time.Now()
	results, updErr := client.Update(ctx, plan.Inputs...)
	for _, r := range results {
		switch {
		case r.Unchanged:
			say(step, toneDim, ui.Linef("• %s already current (%s)", r.Input, ui.Pin(r.OldPin)))
		default:
			moved++
			say(step, toneGood, ui.Linef("✓ %s %s → %s", r.Input, ui.Pin(r.OldPin), ui.Pin(r.NewPin)))
		}
	}
	if results != nil {
		committed, msg, warn := lock.Commit(ctx, client.Root(), results, plan.Commit)
		if committed {
			say(step, toneGood, ui.Linef("committed flake.lock: %s", msg))
		}
		if warn != "" {
			say(step, toneWarn, ui.Linef("! %s", warn))
		}
	}
	if updErr != nil {
		fail(step, t0, updErr)
		return
	}
	emit(runStepEndMsg{step: step, dur: time.Since(t0)})

	// Step: rebuild the local host.
	if plan.LocalSwitch {
		step++
		emit(runStepStartMsg{step: step})
		t0 = time.Now()
		err := proc.RunLines(ctx, client.Root(), logLine(step), "nh", "os", "switch", client.Root())
		if err != nil {
			fail(step, t0, err)
			return
		}
		switched = true
		emit(runStepEndMsg{step: step, dur: time.Since(t0)})
	}

	// Steps: deploy each remote host in order.
	for _, h := range plan.Hosts {
		step++
		emit(runStepStartMsg{step: step})
		t0 = time.Now()
		err := proc.RunLines(ctx, client.Root(), logLine(step), "deploy", h.Argv()...)
		if err != nil {
			fail(step, t0, err)
			return
		}
		deployed = append(deployed, h.Name)
		emit(runStepEndMsg{step: step, dur: time.Since(t0)})
	}

	emit(runEndMsg{detail: detail()})
}

// failLines are the log lines for a step that failed with err: "✗" and the
// whole error, one pane line per line of it, since an error from nix carries
// nix's stderr; then a note when cleaning cut a line short. The step they
// belong to is filled in by at.
func failLines(err error) []runLineMsg {
	lines, rep := ui.ErrLines(err)
	out := make([]runLineMsg, 0, len(lines)+1)
	for i, l := range lines {
		if i == 0 {
			l = ui.Linef("✗ %s", l)
		}
		out = append(out, runLineMsg{text: l, tone: toneBad})
	}
	if note := ui.CutNote(rep.Unterminated); !note.IsEmpty() {
		out = append(out, runLineMsg{text: note, tone: toneWarn})
	}
	return out
}

func (l runLineMsg) at(step int) runLineMsg {
	l.step = step
	return l
}
