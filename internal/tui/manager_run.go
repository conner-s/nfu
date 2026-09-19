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

	"github.com/conner-s/nfu/internal/flake"
	"github.com/conner-s/nfu/internal/lock"
	"github.com/conner-s/nfu/internal/proc"
	"github.com/conner-s/nfu/internal/project"
	"github.com/conner-s/nfu/internal/ui"
)

// stepsForPlan lays a plan out as the run view's step list. Ordering must match
// the engine's emission order: update, then switch (when local), then each host.
func stepsForPlan(plan project.Plan, root string) []runStep {
	commit := ""
	if plan.Commit {
		commit = " + commit flake.lock"
	}
	steps := []runStep{{
		verb:   "update",
		icon:   "↻",
		title:  "update inputs",
		detail: strings.Join(plan.Inputs, ", "),
		cmd:    "nix flake update " + strings.Join(plan.Inputs, " ") + commit,
	}}
	if plan.LocalSwitch {
		steps = append(steps, runStep{
			verb:   "switch",
			icon:   "❄",
			title:  "nh os switch",
			detail: root,
			cmd:    "nh os switch " + root,
		})
	}
	for _, h := range plan.Hosts {
		steps = append(steps, runStep{
			verb:   "deploy",
			icon:   "➤",
			title:  "deploy " + h.Name,
			detail: strings.Join(h.Argv(), " "),
			cmd:    "deploy " + strings.Join(h.Argv(), " "),
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
		return m, m.say("a run is already in flight", toneWarn)
	}
	plan, err := project.BuildPlan(m.deps.Catalog, e.Name, m.deps.Commit)
	if err != nil {
		return m, m.say("✗ "+shortErr(err), toneBad)
	}
	root := m.deps.Client.Root()
	steps := stepsForPlan(plan, root)
	if m.view != mRun {
		m.back = m.view
	}
	m.runNo++

	if dry {
		rs := &runState{name: e.Name, plan: plan, steps: steps, dry: true, no: m.runNo, status: runDry, cur: len(steps) - 1}
		for i, s := range steps {
			rs.lines = append(rs.lines, runLine{step: i, text: "→ " + s.cmd, tone: toneText})
		}
		m.run = rs
		m.view = mRun
		return m, m.say("dry-run · nothing executed", toneWarn)
	}

	runCtx, cancel := context.WithCancel(m.ctx)
	m.runCancel = cancel
	rs := &runState{
		name: e.Name, plan: plan, steps: steps, no: m.runNo,
		status: runRunning, started: time.Now(),
		events: make(chan tea.Msg, 64),
	}
	m.run = rs
	m.view = mRun

	// Pre-authorize sudo for the local switch: tea.ExecProcess hands the real
	// terminal to `sudo -v`, so a password prompt works; with cached credentials
	// it returns instantly. Remote-only plans skip straight to the engine.
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
		rs.lines = append(rs.lines, runLine{step: rs.cur, text: "sudo authorization failed — run aborted", tone: toneBad})
		if m.runCancel != nil {
			m.runCancel()
		}
		return m, m.say("✗ sudo authorization failed", toneBad)
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
	return m, m.say("cancelling…", toneWarn)
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
		return m.say("✗ "+shortErr(err), toneBad)
	}
	return m.say(fmt.Sprintf("✓ %d steps complete", len(rs.steps)), toneGood)
}

// runEngine executes the pipeline, emitting progress events. Step indices match
// stepsForPlan: 0 update, then switch (when local), then one per host. It owns
// no model state — everything flows through ch, ending with exactly one
// runEndMsg.
func runEngine(ctx context.Context, client *flake.Client, plan project.Plan, ch chan<- tea.Msg) {
	emit := func(msg tea.Msg) { ch <- msg }
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
		emit(runLineMsg{step: step, text: "✗ " + err.Error(), tone: toneBad})
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
			emit(runLineMsg{step: step, text: fmt.Sprintf("• %s already current (%s)", r.Input, ui.ShortPin(r.OldPin)), tone: toneDim})
		default:
			moved++
			emit(runLineMsg{step: step, text: fmt.Sprintf("✓ %s %s → %s", r.Input, ui.ShortPin(r.OldPin), ui.ShortPin(r.NewPin)), tone: toneGood})
		}
	}
	if results != nil {
		committed, msg, warn := lock.Commit(ctx, client.Root(), results, plan.Commit)
		if committed {
			emit(runLineMsg{step: step, text: "committed flake.lock: " + msg, tone: toneGood})
		}
		if warn != "" {
			emit(runLineMsg{step: step, text: "! " + warn, tone: toneWarn})
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
		i := step
		err := proc.RunLines(ctx, client.Root(), func(l string) {
			emit(runLineMsg{step: i, text: l, tone: toneDim})
		}, "nh", "os", "switch", client.Root())
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
		i := step
		argv := h.Argv()
		err := proc.RunLines(ctx, client.Root(), func(l string) {
			emit(runLineMsg{step: i, text: l, tone: toneDim})
		}, "deploy", argv...)
		if err != nil {
			fail(step, t0, err)
			return
		}
		deployed = append(deployed, h.Name)
		emit(runStepEndMsg{step: step, dur: time.Since(t0)})
	}

	emit(runEndMsg{detail: detail()})
}
