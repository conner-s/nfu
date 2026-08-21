package tui

import (
	"fmt"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/conner-s/nfu/internal/flake"
	"github.com/conner-s/nfu/internal/ui"
)

func (m model) View() string {
	s := m.styles
	switch m.state {
	case stateLoading:
		return fmt.Sprintf("\n  %s %s\n", m.spinner.View(), s.Dim.Render("reading flake inputs…"))

	case statePicking:
		return m.pickView()

	case stateUpdating:
		return fmt.Sprintf("\n  %s %s\n", m.spinner.View(),
			s.Value.Render("updating ")+s.Dim.Render(m.buildUpdatingLabel())+s.Dim.Render(" …"))

	case stateDone:
		return m.doneView()
	}
	return ""
}

func (m model) header() string {
	s := m.styles
	root := m.client.Root()
	return fmt.Sprintf("%s  %s\n\n", s.Title.Render(filepath.Base(root)), s.Dim.Render(root))
}

func (m model) pickView() string {
	s := m.styles
	var b strings.Builder
	b.WriteString(m.header())

	// Column widths from the plain text. The ref+age "tail" is padded to a shared
	// width too, but only across rows that carry an upstream segment, so those
	// segments line up into a column under `F` without padding segment-less rows.
	var wn, wt, wp, wr int
	tails := make([]string, len(m.inputs))
	segs := make([]string, len(m.inputs))
	for i, in := range m.inputs {
		wn = maxi(wn, utf8.RuneCountInString(in.Name))
		wt = maxi(wt, utf8.RuneCountInString(in.Type))
		wp = maxi(wp, utf8.RuneCountInString(ui.ShortPin(in.Pin())))
		ref := in.Ref
		if in.Follows {
			ref = "follows"
		}
		tails[i] = strings.TrimSpace(ref + "  " + ui.RelAge(in.LastModified, m.now))
		segs[i] = m.upstreamSegment(in)
		if segs[i] != "" {
			wr = maxi(wr, utf8.RuneCountInString(tails[i]))
		}
	}

	for i, in := range m.inputs {
		cursor := "  "
		if i == m.cursor {
			cursor = s.Title.Render("❯ ")
		}
		var box string
		switch {
		case in.Follows:
			box = s.Dim.Render(ui.GlyphFollows + " ")
		case m.selected[i]:
			box = s.Good.Render("[x]")
		default:
			box = s.Dim.Render("[ ]")
		}
		name := padRight(in.Name, wn)
		if in.Follows {
			name = s.Dim.Render(name)
		} else if m.selected[i] {
			name = s.Good.Render(name)
		} else {
			name = s.Value.Render(name)
		}
		tail := tails[i]
		if segs[i] != "" {
			tail = padRight(tail, wr) // align the segment column
		}
		line := fmt.Sprintf("%s%s %s  %s  %s  %s",
			cursor, box, name,
			s.Label.Render(padRight(in.Type, wt)),
			s.Value.Render(padRight(ui.ShortPin(in.Pin()), wp)),
			s.Dim.Render(tail),
		)
		if segs[i] != "" {
			line += "  " + segs[i]
		}
		b.WriteString(strings.TrimRight(line, " ") + "\n")
	}

	n := len(m.selectedNames())
	b.WriteString("\n")
	b.WriteString(m.commitToggle())
	b.WriteString("\n")
	b.WriteString(s.Help.Render(fmt.Sprintf(
		"↑/↓ move · space select · a all · f/F upstream · r/R re-probe · c commit · enter update (%d) · q quit", n)))
	b.WriteString("\n")
	return b.String()
}

// upstreamSegment renders the trailing upstream-probe column for one row, empty
// until the input has been checked (`f`/`F`). While a probe is in flight it shows
// a spinner; on success it shows either "up to date" or the moved pin with both
// its relative age and absolute timestamp (the diff the hotkeys exist to surface);
// on failure it shows a short error marker.
func (m model) upstreamSegment(in flake.Input) string {
	s := m.styles
	name := in.Name
	if m.checking[name] {
		return m.spinner.View() + s.Dim.Render("checking upstream…")
	}
	if e, ok := m.upstreamErr[name]; ok {
		return s.Warn.Render("! " + e)
	}
	up, ok := m.upstream[name]
	if !ok {
		return ""
	}
	if up.Pin() == in.Pin() {
		return s.Dim.Render(ui.GlyphUnchanged + " up to date")
	}
	return fmt.Sprintf("%s %s  %s",
		s.Dim.Render(ui.GlyphArrow),
		s.Good.Render(ui.ShortPin(up.Pin())),
		s.Dim.Render(ui.RelAge(up.LastModified, m.now)+" ("+ui.AbsTime(up.LastModified)+")"),
	)
}

// commitToggle renders the commit switch: whether a successful update will also
// git-commit flake.lock. It mirrors the row checkbox idiom ([x]/[ ]) so the
// toggle reads at a glance, and reflects the invoked state until `c` flips it.
func (m model) commitToggle() string {
	s := m.styles
	if m.commit {
		return "  " + s.Good.Render("[x] commit flake.lock")
	}
	return "  " + s.Dim.Render("[ ] commit flake.lock")
}

func (m model) doneView() string {
	s := m.styles
	if m.err != nil && m.results == nil {
		// Load failure: render nothing. Run returns the error and main prints it
		// once as `nfu: <err>` — rendering it here too would double it up.
		return ""
	}
	if m.canceled && m.results == nil {
		// Cancelled from the picker (or nothing selectable): leave the input list
		// on screen, nothing more.
		return m.header() + ui.RenderInputList(s, m.inputs, m.now) + "\n" + s.Dim.Render("(no changes)") + "\n"
	}
	var b strings.Builder
	b.WriteString(m.header())
	b.WriteString(ui.RenderResults(s, m.results, m.now))
	b.WriteString("\n")

	moved := 0
	for _, r := range m.results {
		if r.Moved() {
			moved++
		}
	}
	switch {
	case m.err != nil:
		// An update that failed: show what moved, but not a cheerful summary.
		// Run returns m.err; main prints the actual error.
		b.WriteString(s.Bad.Render("update did not complete") + "\n")
	case moved == 0:
		b.WriteString(s.Dim.Render("everything already up to date") + "\n")
	case moved == 1:
		b.WriteString(s.Value.Render("updated 1 input") + "\n")
	default:
		b.WriteString(s.Value.Render(fmt.Sprintf("updated %d inputs", moved)) + "\n")
	}
	if m.committed {
		b.WriteString(s.Good.Render("committed flake.lock:") + " " + s.Dim.Render(m.commitMsg) + "\n")
	}
	if m.warn != "" {
		b.WriteString(s.Warn.Render("! "+m.warn) + "\n")
	}
	return b.String()
}

func padRight(s string, n int) string {
	if w := utf8.RuneCountInString(s); w < n {
		return s + strings.Repeat(" ", n-w)
	}
	return s
}

func maxi(a, b int) int {
	if a > b {
		return a
	}
	return b
}
