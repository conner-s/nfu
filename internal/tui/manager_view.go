// Rendering for the project manager. Each view follows the Claude Design
// prototype: list (table + filter), detail (steps timeline beside the on-disk
// TOML), run (step sidebar + live log + progress bar), with a shared header and
// key-hint footer.
//
// Nearly everything these views draw comes from project files, the run
// history and the configuration, and none of it is drawn raw. A view reads an
// entryRow (rowFor), a runStep (stepsForPlan), a runLine (runEngine) or the
// toast (say), whose text is termsafe.Text; the project.Entry values on the
// model are for acting on. docs/untrusted-text.md has the whole table.
package tui

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"forgejo.kube.internal.conn3r.xyz/connerlab/termsafe"

	"github.com/conner-s/nfu/internal/project"
	"github.com/conner-s/nfu/internal/ui"
)

// View renders the manager into the alt screen — this is the one nfu model
// that takes it (bubbletea v2 replaced tea.WithAltScreen with this field).
func (m managerModel) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	return v
}

func (m managerModel) render() string {
	if m.quit {
		return ""
	}
	w, h := m.dims()
	var body string
	switch m.view {
	case mList:
		body = m.viewProjectList(w, h)
	case mDetail:
		body = m.viewProjectDetail(w, h)
	case mRun:
		body = m.viewRun(w, h)
	case mEdit:
		if m.form != nil {
			// The form is also a root program elsewhere; embedded here it must
			// contribute a body, not its own tea.View.
			body = m.form.render()
		}
	case mEditLoading:
		body = fmt.Sprintf("\n  %s %s\n", m.spin.View(), m.g.Dim.Render("reading flake inputs…"))
	}
	footer := m.viewFooter(w)
	// Pin the footer to the bottom of the alt screen.
	gap := h - lipgloss.Height(body) - lipgloss.Height(footer)
	if gap < 0 {
		gap = 0
	}
	return body + strings.Repeat("\n", gap) + footer
}

// dims returns the usable width/height, with a sane fallback before the first
// WindowSizeMsg arrives.
func (m managerModel) dims() (int, int) {
	w, h := m.w, m.h
	if w <= 0 {
		w = 100
	}
	if h <= 0 {
		h = 30
	}
	return w, h
}

// --- shared chrome ---------------------------------------------------------

func (m managerModel) viewHeader(w int) string {
	g := m.g
	left := g.Badge.Render("nfu") + " " + g.Gray.Render("project")
	mid := g.Faint.Render(" │ ") + g.Tan.Render(fmt.Sprintf("projects(%d)", len(m.filteredIdx()))) +
		g.Faint.Render(" │ ") + g.Gray.Render(ui.Line(m.deps.ProjectsDir).String())
	right := g.Faint.Render("no current project")
	if m.current != "" {
		right = g.Gray.Render("current: ") + g.Yellow.Render(ui.Name(m.current).String())
	}
	line := left + mid
	pad := w - lipgloss.Width(line) - lipgloss.Width(right)
	if pad < 1 {
		return line + "\n"
	}
	return line + strings.Repeat(" ", pad) + right + "\n"
}

// footerHints are the per-view key bindings shown in the bottom bar.
func (m managerModel) footerHints() [][2]string {
	if m.run != nil && m.view == mRun {
		switch m.run.status {
		case runRunning:
			return [][2]string{{"ctrl-c", "cancel"}}
		case runDry:
			return [][2]string{{"r", "run for real"}, {"esc", "back"}}
		case runFail:
			return [][2]string{{"r", "retry"}, {"d", "dry-run"}, {"esc", "back"}}
		default:
			return [][2]string{{"enter", "back"}, {"r", "run again"}}
		}
	}
	switch m.view {
	case mDetail:
		return [][2]string{{"r", "run"}, {"d", "dry-run"}, {"e", "edit"}, {"E", "$EDITOR"}, {"u", "use"}, {"h/l", "project"}, {"esc", "back"}}
	case mEditLoading:
		return [][2]string{{"esc", "cancel"}}
	case mEdit:
		return nil // the form renders its own footer
	default:
		return [][2]string{{"j/k", "move"}, {"enter", "open"}, {"r", "run"}, {"d", "dry-run"}, {"e", "edit"}, {"n", "new"}, {"u", "use"}, {"/", "filter"}, {"q", "quit"}}
	}
}

func (m managerModel) viewFooter(w int) string {
	g := m.g
	var parts []string
	for _, hint := range m.footerHints() {
		parts = append(parts, g.Yellow.Render(hint[0])+" "+g.Gray.Render(hint[1]))
	}
	line := " " + strings.Join(parts, g.Faint.Render(" · "))
	if !m.toast.IsEmpty() {
		t := m.toneStyle(m.toastTone)(m.toast.String())
		pad := w - lipgloss.Width(line) - lipgloss.Width(t) - 1
		if pad > 0 {
			return line + strings.Repeat(" ", pad) + t
		}
		// No room beside the hints. A toast that ran off the right edge
		// would be cut there, and a warning must be read to the end: it
		// gets lines of its own above the hints, wrapped to the width.
		var above strings.Builder
		wrapped := lipgloss.NewStyle().Width(maxi(w-2, 20)).Render(m.toast.String())
		for _, l := range strings.Split(wrapped, "\n") {
			above.WriteString(" " + m.toneStyle(m.toastTone)(strings.TrimRight(l, " ")) + "\n")
		}
		return above.String() + line
	}
	return line
}

// --- list view -------------------------------------------------------------

func (m managerModel) viewProjectList(w, h int) string {
	g := m.g
	var b strings.Builder
	b.WriteString("\n")
	b.WriteString(" " + m.viewHeader(w-2))

	filter := ui.Line(m.filter)
	if m.filterOn || m.filter != "" {
		cursor := ""
		if m.filterOn {
			cursor = g.Fg.Render("▏")
		}
		b.WriteString(" " + g.Yellow.Render("/") + g.Fg.Render(filter.String()) + cursor +
			g.Faint.Render("  esc clear · enter keep") + "\n")
	}
	b.WriteString("\n")

	idx := m.filteredIdx()
	if len(m.entries) == 0 {
		b.WriteString(" " + g.Faint.Render("no projects yet — `n` to create one") + "\n")
		return b.String()
	}
	if len(idx) == 0 {
		b.WriteString(" " + g.Faint.Render(fmt.Sprintf("no projects match '%s'", filter)) + "\n")
		return b.String()
	}

	// Column widths from content; DESCRIPTION flexes to the remaining width.
	rows := make([]entryRow, len(idx))
	wName, wTarget, wLast := 4, 6, 8
	for i, n := range idx {
		rows[i] = m.rowFor(&m.entries[n])
		wName = maxi(wName, utf8.RuneCountInString(rows[i].name.String()))
		wTarget = maxi(wTarget, utf8.RuneCountInString(rows[i].target.String()))
		wLast = maxi(wLast, utf8.RuneCountInString(rows[i].lastRun))
	}
	const wSteps, wStatus = 5, 8
	wDesc := w - (2 + 1 + wName + 2 + wSteps + 2 + wTarget + 2 + wLast + 2 + wStatus + 4)
	if wDesc < 10 {
		wDesc = 10
	}

	hdr := fmt.Sprintf("   %s  %s  %s  %s  %s  %s",
		padRight("NAME", wName), padRight("DESCRIPTION", wDesc), padRight("STEPS", wSteps),
		padRight("TARGET", wTarget), padRight("LAST RUN", wLast), "STATUS")
	b.WriteString(g.Yellow.Bold(true).Render(hdr) + "\n")

	// Scroll window: keep the selection visible in the rows the height allows.
	visible := h - 9
	if visible < 3 {
		visible = 3
	}
	pos := 0
	for i, n := range idx {
		if n == m.sel {
			pos = i
		}
	}
	top := clamp(m.top, 0, maxi(0, len(idx)-visible))
	if pos < top {
		top = pos
	}
	if pos >= top+visible {
		top = pos - visible + 1
	}

	for i := top; i < mini(len(idx), top+visible); i++ {
		r := rows[i]
		on := idx[i] == m.sel
		marker := " "
		if r.current {
			marker = g.Yellow.Render("*")
		}
		cursor := "  "
		if on {
			cursor = g.Orange.Render("▸ ")
		}
		line := fmt.Sprintf("%s%s%s  %s  %s  %s  %s  %s",
			marker, cursor,
			g.Fg.Bold(true).Render(padRight(r.name.String(), wName)),
			m.dimIf(!on, padRight(r.desc.Truncate(wDesc).String(), wDesc)),
			g.Tan.Render(padRight(fmt.Sprintf("%d", r.steps), wSteps)),
			m.toneStyle(r.targetTone)(padRight(r.target.String(), wTarget)),
			g.Tan.Render(padRight(r.lastRun, wLast)),
			m.toneStyle(r.statusTone)(r.status),
		)
		b.WriteString(strings.TrimRight(line, " ") + "\n")
	}
	if len(idx) > visible {
		b.WriteString(" " + g.Faint.Render(fmt.Sprintf("… %d of %d shown", visible, len(idx))) + "\n")
	}
	return b.String()
}

// entryRow is a project as the manager draws it. A project.Entry is read from
// TOML files anyone may have written, so the views never draw its fields: they
// draw a row, whose text was cleaned when rowFor built it. source, lastRun and
// status are nfu's own words.
type entryRow struct {
	name       termsafe.Text
	desc       termsafe.Text // empty when the project has none
	source     string
	current    bool
	steps      int
	target     termsafe.Text
	targetTone tone
	ran        bool // the run history has a run of it
	lastRun    string
	status     string
	statusTone tone

	// For the detail view.
	lastDetail termsafe.Text // what the last run did, from the run history
	flake      termsafe.Text
	bundle     termsafe.Text // the projects it includes; empty for a plain project
	inputs     termsafe.Text // what a run would move
}

// rowFor cleans one entry for display. This is the one place a project's
// name, description, hosts, inputs and paths become text to draw.
func (m managerModel) rowFor(e *project.Entry) entryRow {
	inputs, hosts, localSwitch := m.shape(e)
	r := entryRow{
		name:    ui.Name(e.Name),
		desc:    ui.Line(e.Project.Description),
		source:  e.Source,
		current: e.Name == m.current,
		steps:   stepCount(hosts, localSwitch),
		lastRun: m.lastRunLabel(e.Name),
		flake:   ui.Line(m.flakeRootFor(e)),
		inputs:  ui.Names(inputs, ", "),
	}
	r.target, r.targetTone = targetLabel(hosts, localSwitch)
	r.status, r.statusTone = m.statusLabel(e.Name)
	if rec, ok := m.runs[e.Name]; ok && !rec.At.IsZero() {
		r.ran = true
		r.lastDetail = ui.Line(rec.Detail)
	}
	if len(e.Project.Includes) > 0 {
		r.bundle = ui.Names(e.Project.Includes, " + ")
	}
	return r
}

func (m managerModel) dimIf(dim bool, s string) string {
	if dim {
		return m.g.Gray.Render(s)
	}
	return m.g.Fg.Render(s)
}

func (m managerModel) lastRunLabel(name string) string {
	rec, ok := m.runs[name]
	if !ok || rec.At.IsZero() {
		return "—"
	}
	return ui.RelAge(rec.At.Unix(), m.now)
}

func (m managerModel) statusLabel(name string) (string, tone) {
	rec, ok := m.runs[name]
	if !ok || rec.At.IsZero() {
		return "—", toneDim
	}
	if rec.OK {
		return "✓ ok", toneGood
	}
	return "✗ failed", toneBad
}

// --- detail view -----------------------------------------------------------

func (m managerModel) viewProjectDetail(w, h int) string {
	e := m.curEntry()
	if e == nil {
		return "\n " + m.g.Faint.Render("no project selected") + "\n"
	}
	r := m.rowFor(e)
	g := m.g
	leftW := w * 3 / 5
	rightW := w - leftW - 3
	if rightW < 24 {
		leftW, rightW = w-2, 0
	}

	var b strings.Builder
	rows := m.filteredIdx()
	pos := 1
	for i, idx := range rows {
		if idx == m.sel {
			pos = i + 1
		}
	}

	// Title: name, source badge, position among the (filtered) projects.
	title := g.Orange.Bold(true).Render(r.name.String()) + "  " +
		g.Yellow.Render("["+r.source+"]") + "  " +
		g.Faint.Render(fmt.Sprintf("‹ %d of %d ›", pos, len(rows)))
	b.WriteString(title + "\n")
	desc := r.desc.String()
	if r.desc.IsEmpty() {
		desc = "no description"
	}
	b.WriteString(g.Tan.Render(desc) + "\n\n")

	// Meta box: where a run lands and what it moves.
	last := r.status
	if r.ran {
		last += " · " + r.lastRun
		if !r.lastDetail.IsEmpty() {
			last += "  (" + r.lastDetail.String() + ")"
		}
	}
	kv := func(k, v string) string { return g.Gray.Render(padRight(k, 9)) + " " + v }
	// `inputs` shows what a run would MOVE (m.shape resolves a bundle's includes),
	// and `bundle` names the members it came from, only when there are any.
	metaRows := []string{
		kv("flake", g.Tan.Render(r.flake.String())),
		kv("target", m.toneStyle(r.targetTone)(r.target.String())),
	}
	if !r.bundle.IsEmpty() {
		metaRows = append(metaRows, kv("bundle", g.Purple.Render(r.bundle.String())))
	}
	metaRows = append(metaRows,
		kv("inputs", g.Blue.Render(r.inputs.String())),
		kv("last run", m.toneStyle(r.statusTone)(last)),
	)
	meta := strings.Join(metaRows, "\n")
	b.WriteString(g.Box.Width(leftW).Render(meta) + "\n\n")

	// Steps timeline.
	plan, err := project.BuildPlan(m.deps.Catalog, e.Name, m.deps.Commit)
	if err != nil {
		b.WriteString(g.Red.Render(ui.Linef("✗ %s", err).String()) + "\n")
	} else {
		steps := stepsForPlan(plan, m.flakeRootFor(e))
		b.WriteString(g.Purple.Bold(true).Render(fmt.Sprintf("STEPS · %d", len(steps))) + "\n")
		for i, s := range steps {
			c := m.stepStyle(s.verb)
			b.WriteString(fmt.Sprintf(" %s  %s %s %s\n",
				c.Render(fmt.Sprintf("%d", i+1)),
				c.Render(s.icon+" "+s.title.String()),
				g.Faint.Render("·"),
				g.Tan.Render(s.detail.Truncate(leftW-utf8.RuneCountInString(s.title.String())-10).String())))
			rail := " " + g.Faint.Render("│")
			if i == len(steps)-1 {
				rail = "  "
			}
			b.WriteString(rail + "   " + g.Faint.Render(s.cmd.Truncate(leftW-6).String()) + "\n")
		}
		// A name in the plan that is not what it looks like, before `r` runs it.
		for _, n := range plan.Notes() {
			b.WriteString(g.Yellow.Render("! "+n.Truncate(leftW-3).String()) + "\n")
		}
	}
	left := b.String()
	if rightW <= 0 {
		return "\n" + left
	}

	right := m.viewTomlPane(e, rightW)
	joined := lipgloss.JoinHorizontal(lipgloss.Top,
		lipgloss.NewStyle().Width(leftW).Render(left),
		lipgloss.NewStyle().Width(rightW).Render(right))
	return "\n " + strings.ReplaceAll(joined, "\n", "\n ") + "\n"
}

func (m managerModel) flakeRootFor(e *project.Entry) string {
	if e.Project.FlakeRoot != "" {
		return e.Project.FlakeRoot
	}
	return m.deps.Client.Root()
}

func (m managerModel) stepStyle(verb string) lipgloss.Style {
	switch verb {
	case "update":
		return m.g.Blue
	case "switch":
		return m.g.Aqua
	default:
		return m.g.Orange
	}
}

// viewTomlPane renders the project's definition as it would sit on disk, with a
// light TOML tint. It re-marshals the in-memory project, so it is faithful for
// declared projects too (which have no standalone file).
//
// The TOML is cleaned before it is drawn. A TOML encoder escapes the C0
// controls in a string, but a C1 control, a bidirectional override or an
// invisible character is legal TOML and comes out as it went in; here each is
// shown as a marker, so the pane differs from the file exactly where the file
// holds one.
func (m managerModel) viewTomlPane(e *project.Entry, w int) string {
	g := m.g
	var b strings.Builder
	title := ui.Linef("%s.TOML", strings.ToUpper(e.Name))
	note := " · e edit"
	if e.Source == project.SourceDeclared {
		note = " · read-only"
	}
	b.WriteString(g.Gray.Render(title.String()) + g.Faint.Render(note) + "\n\n")
	data, err := project.MarshalProject(e.Project)
	if err != nil {
		return b.String() + g.Red.Render(ui.Linef("%s", err).String())
	}
	for _, ln := range tomlLines(data) {
		b.WriteString(m.tintToml(ln.Truncate(w-1)) + "\n")
	}
	return b.String()
}

// tomlLines cleans a marshalled project and splits it into lines. Cleaning
// comes first: a lone carriage return is a line break too.
func tomlLines(data []byte) []termsafe.Text {
	text := termsafe.Mark.Clean(strings.TrimRight(string(data), "\n"))
	parts := strings.Split(text.String(), "\n")
	lines := make([]termsafe.Text, len(parts))
	for i, p := range parts {
		lines[i] = termsafe.Mark.Line(p)
	}
	return lines
}

// tintToml colors one TOML line: comments faint, table headers orange, keys
// aqua, values green.
func (m managerModel) tintToml(line termsafe.Text) string {
	g := m.g
	ln := line.String()
	t := strings.TrimSpace(ln)
	switch {
	case t == "":
		return ""
	case strings.HasPrefix(t, "#"):
		return g.Faint.Render(ln)
	case strings.HasPrefix(t, "["):
		return g.Orange.Render(ln)
	}
	if k, v, ok := strings.Cut(ln, "="); ok {
		return g.Aqua.Render(k) + g.Gray.Render("=") + g.Green.Render(v)
	}
	return g.Tan.Render(ln)
}

// --- run view --------------------------------------------------------------

func (m managerModel) viewRun(w, h int) string {
	rs := m.run
	if rs == nil {
		return ""
	}
	g := m.g
	sideW := 34
	logW := w - sideW - 4
	if logW < 20 {
		sideW, logW = w-2, 0
	}

	// Left: the step list with live status glyphs.
	var side strings.Builder
	mode := fmt.Sprintf("run #%d", rs.no)
	if rs.dry {
		mode = "dry-run"
	}
	side.WriteString(g.Gray.Render(fmt.Sprintf("STEPS · %s · %s", rs.title, mode)) + "\n\n")
	doneN := 0
	for i, s := range rs.steps {
		icon, ic := "○", g.Faint
		t := ""
		switch {
		case s.failed:
			icon, ic = "✗", g.Red
			t = fmtDur(s.dur)
		case s.done:
			icon, ic = "✓", g.Green
			t = fmtDur(s.dur)
			doneN++
		case rs.status == runDry:
			icon, ic = "→", g.Tan
		case i == rs.cur && rs.status == runRunning:
			icon, ic = strings.TrimSpace(m.spin.View()), g.Yellow
			t = fmtDur(time.Since(rs.started))
			if rs.waitingSudo {
				t = "sudo…"
			}
		}
		label := m.stepStyle(s.verb).Render(s.icon + " " + s.title.String())
		if i == rs.cur && rs.status == runRunning {
			label = g.Fg.Bold(true).Render(s.icon + " " + s.title.String())
		}
		side.WriteString(fmt.Sprintf(" %s %s %s\n", ic.Render(icon), padRight(label, sideW-12), g.Yellow.Render(t)))
	}
	prog := fmt.Sprintf("%d/%d", doneN, len(rs.steps))
	if rs.status == runDry {
		prog = fmt.Sprintf("%d/%d", len(rs.steps), len(rs.steps))
	}
	statusTxt, statusStyle := m.runStatusLabel(rs)
	info := strings.Join([]string{
		g.Gray.Render(mode) + g.Faint.Render(" · ") + g.Yellow.Render(prog),
		g.Gray.Render("elapsed ") + g.Fg.Render(fmtDur(rs.elapsed)),
		g.Gray.Render("status  ") + statusStyle.Render(statusTxt),
	}, "\n")
	side.WriteString("\n" + g.Box.Width(sideW).Render(info))

	// Right: the current step's header and its log tail (a dry run shows every
	// step's would-be command instead).
	var right strings.Builder
	cur := rs.steps[clamp(rs.cur, 0, len(rs.steps)-1)]
	head := "● " + cur.icon + " " + cur.title.String() + "  "
	right.WriteString(statusStyle.Render(head) + g.Faint.Render(cur.cmd.Truncate(logW-lipgloss.Width(head)).String()) + "\n\n")
	tail := h - 8
	if tail < 3 {
		tail = 3
	}
	var visible []runLine
	for _, l := range rs.lines {
		if rs.dry || rs.status != runRunning || l.step == rs.cur {
			visible = append(visible, l)
		}
	}
	if len(visible) > tail {
		visible = visible[len(visible)-tail:]
	}
	for _, l := range visible {
		right.WriteString(m.toneStyle(l.tone)(l.text.Truncate(logW).String()) + "\n")
	}
	if rs.status == runRunning {
		right.WriteString(g.Fg.Render("▏"))
	}

	var body string
	if logW > 0 {
		body = lipgloss.JoinHorizontal(lipgloss.Top,
			lipgloss.NewStyle().Width(sideW).Render(side.String()),
			lipgloss.NewStyle().Width(logW).Render(right.String()))
	} else {
		body = side.String()
	}

	return "\n " + strings.ReplaceAll(body, "\n", "\n ") + "\n" + m.viewRunBar(rs, w, doneN)
}

func (m managerModel) runStatusLabel(rs *runState) (string, lipgloss.Style) {
	switch rs.status {
	case runOK:
		return "✓ complete", m.g.Green
	case runFail:
		if rs.canceled {
			return "✗ cancelled", m.g.Red
		}
		return "✗ failed", m.g.Red
	case runDry:
		return "nothing executed", m.g.Yellow
	default:
		return "running", m.g.Yellow
	}
}

// viewRunBar renders the bottom progress bar: done steps solid green, the
// in-flight step pulsing yellow (red once failed), the rest unlit.
func (m managerModel) viewRunBar(rs *runState, w, doneN int) string {
	g := m.g
	const total = 24
	n := len(rs.steps)
	if n == 0 {
		n = 1
	}
	a := total * doneN / n
	var bar, pct string
	switch rs.status {
	case runDry:
		bar = g.Faint.Render(strings.Repeat("█", total))
		pct = "100%"
	default:
		active := 0
		activeStyle := g.Yellow
		if rs.status == runRunning || rs.status == runFail {
			active = mini(3, total-a)
			if rs.status == runFail {
				activeStyle = g.Red
			}
		}
		frac := float64(doneN) / float64(n)
		if rs.status == runRunning {
			frac += 0.35 / float64(n)
		}
		if rs.status == runOK {
			a, active, frac = total, 0, 1
		}
		bar = g.Green.Render(strings.Repeat("█", a)) +
			activeStyle.Render(strings.Repeat("█", active)) +
			g.Faint.Render(strings.Repeat("░", maxi(0, total-a-active)))
		pct = fmt.Sprintf("%d%%", int(frac*100))
	}
	return fmt.Sprintf(" %s %s\n", bar, g.Yellow.Render(pct))
}

// --- small helpers ---------------------------------------------------------

func mini(a, b int) int {
	if a < b {
		return a
	}
	return b
}
