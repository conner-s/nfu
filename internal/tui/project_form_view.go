package tui

import (
	"fmt"
	"strings"

	"github.com/conner-s/nfu/internal/ui"
)

func (m formModel) View() string {
	if m.quit {
		return ""
	}
	s := m.styles
	var b strings.Builder

	title := "new project"
	if m.editing {
		title = "edit project"
	}
	fmt.Fprintf(&b, "%s\n\n", s.Title.Render(title))

	b.WriteString(m.viewName(s))
	b.WriteString(m.viewInputs(s))
	b.WriteString(m.viewSwitch(s))
	b.WriteString(m.viewHosts(s))

	if m.err != "" {
		fmt.Fprintf(&b, "\n%s\n", s.Bad.Render("✗ "+m.err))
	}
	fmt.Fprintf(&b, "\n%s\n", s.Help.Render(m.footer()))
	return b.String()
}

// sectionLabel renders a section heading, marking the active one.
func (m formModel) sectionLabel(s ui.Styles, sec section, text string) string {
	if m.section == sec && !m.hostEdit {
		return s.Title.Render("▸ " + text)
	}
	return s.Label.Render("  " + text)
}

func (m formModel) viewName(s ui.Styles) string {
	val := m.name.View()
	if m.editing {
		val = s.Value.Render(m.name.Value()) + s.Dim.Render("  (fixed)")
	}
	return fmt.Sprintf("%s  %s\n\n", m.sectionLabel(s, secName, "name"), val)
}

func (m formModel) viewInputs(s ui.Styles) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n", m.sectionLabel(s, secInputs, "inputs"))
	sel := m.selectableInputs()
	if len(sel) == 0 {
		fmt.Fprintf(&b, "    %s\n\n", s.Dim.Render("no inputs found"))
		return b.String()
	}
	for i, in := range sel {
		box := "[ ]"
		if m.inSel[in.Name] {
			box = s.Good.Render("[x]")
		}
		cursor := "  "
		if m.section == secInputs && !m.hostEdit && i == m.inCursor {
			cursor = s.Value.Render("▸ ")
		}
		name := s.Value.Render(in.Name)
		if !m.inSel[in.Name] {
			name = s.Label.Render(in.Name)
		}
		fmt.Fprintf(&b, "  %s%s %s\n", cursor, box, name)
	}
	b.WriteString("\n")
	return b.String()
}

func (m formModel) viewSwitch(s ui.Styles) string {
	state := s.Dim.Render("off — remote only")
	if m.localSwitch {
		state = s.Good.Render("on — nh os switch")
	}
	return fmt.Sprintf("%s  %s\n\n", m.sectionLabel(s, secSwitch, "local switch"), state)
}

func (m formModel) viewHosts(s ui.Styles) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n", m.sectionLabel(s, secHosts, "deploy hosts"))
	if len(m.hosts) == 0 {
		fmt.Fprintf(&b, "    %s\n", s.Dim.Render("none — 'a' to add"))
	}
	for i, h := range m.hosts {
		box := "[ ]"
		if h.selected {
			box = s.Good.Render("[x]")
		}
		cursor := "  "
		if m.section == secHosts && !m.hostEdit && i == m.hCursor {
			cursor = s.Value.Render("▸ ")
		}
		args := ""
		if len(h.args) > 0 {
			args = s.Dim.Render("  " + joinArgs(h.args))
		}
		tag := ""
		if !h.known {
			tag = s.Dim.Render("  (custom)")
		}
		fmt.Fprintf(&b, "  %s%s %s%s%s\n", cursor, box, s.Value.Render(h.name), args, tag)
	}
	if m.hostEdit {
		b.WriteString(m.viewHostEdit(s))
	}
	b.WriteString("\n")
	return b.String()
}

func (m formModel) viewHostEdit(s ui.Styles) string {
	verb := "add host"
	if m.editIdx >= 0 {
		verb = "edit args"
	}
	return fmt.Sprintf("    %s\n    %s %s\n    %s %s\n",
		s.Title.Render("· "+verb+" ·"),
		s.Label.Render("name:"), m.hName.View(),
		s.Label.Render("args:"), m.hArgs.View(),
	)
}

func (m formModel) footer() string {
	if m.hostEdit {
		return "tab switch field · enter confirm · esc cancel"
	}
	switch m.section {
	case secInputs:
		return "↑/↓ move · space toggle · tab next section · ctrl+s save · esc cancel"
	case secSwitch:
		return "space toggle · tab next section · ctrl+s save · esc cancel"
	case secHosts:
		return "↑/↓ move · space toggle · a add · e edit args · d delete custom · tab next · ctrl+s save"
	default: // name
		return "type name · tab next section · ctrl+s save · esc cancel"
	}
}
