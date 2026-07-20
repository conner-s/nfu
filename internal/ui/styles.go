// Package ui renders nfu's terminal output. Colors come exclusively from the
// theme registry (internal/theme) — the `theme` config key picks the palette,
// and lipgloss auto-strips color when stdout is not a TTY, so piped and --json
// output stays clean.
package ui

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/conner-s/nfu/internal/theme"
)

// Styles centralizes lipgloss styling so callers stay declarative.
type Styles struct {
	Title lipgloss.Style
	Label lipgloss.Style
	Value lipgloss.Style
	Dim   lipgloss.Style
	Good  lipgloss.Style
	Warn  lipgloss.Style
	Bad   lipgloss.Style
	Help  lipgloss.Style
}

// NewStyles builds the default-theme palette (tests and themeless callers).
func NewStyles() Styles {
	t, _ := theme.Get("")
	return StylesFromTheme(t)
}

// StylesFor builds the palette for the named theme. The name is validated at
// config load; an unknown name here falls back to the default as belt and
// braces rather than panicking mid-render.
func StylesFor(name string) Styles {
	t, err := theme.Get(name)
	if err != nil {
		t, _ = theme.Get("")
	}
	return StylesFromTheme(t)
}

// StylesFromTheme maps a theme onto the line-oriented style set. Structural
// attributes (bold) live here on purpose: themes recolor, they do not relayout.
func StylesFromTheme(t theme.Theme) Styles {
	c := func(v string) lipgloss.Color { return lipgloss.Color(v) }
	return Styles{
		Title: lipgloss.NewStyle().Bold(true).Foreground(c(t.Title)),
		Label: lipgloss.NewStyle().Foreground(c(t.Label)),
		Value: lipgloss.NewStyle().Foreground(c(t.Value)),
		Dim:   lipgloss.NewStyle().Foreground(c(t.Dim)),
		Good:  lipgloss.NewStyle().Foreground(c(t.Good)),
		Warn:  lipgloss.NewStyle().Bold(true).Foreground(c(t.Warn)),
		Bad:   lipgloss.NewStyle().Bold(true).Foreground(c(t.Bad)),
		Help:  lipgloss.NewStyle().Foreground(c(t.Help)),
	}
}

// Status glyphs used across list/update rendering.
const (
	GlyphMoved     = "✓" // an input's pin moved
	GlyphUnchanged = "•" // already current
	GlyphError     = "✗" // failed
	GlyphFollows   = "↳" // a follows alias
	GlyphArrow     = "→"
)

// Gruv is the palette the full-screen project manager paints with. The manager
// owns the whole alt-screen (panes, selection bars, a log tail), so it carries
// a richer style set than the line-oriented Styles above. The field names keep
// the gruvbox color vocabulary the views were written in; each maps to a
// semantic theme slot, so non-gruvbox themes recolor it wholesale.
type Gruv struct {
	Orange lipgloss.Style // accents, selection marker, deploy steps
	Yellow lipgloss.Style // key hints, table headers, in-flight state
	Green  lipgloss.Style // ok / done
	Red    lipgloss.Style // failed
	Blue   lipgloss.Style // ssh targets, update steps
	Aqua   lipgloss.Style // local targets, switch steps, toml keys
	Purple lipgloss.Style // section titles
	Fg     lipgloss.Style // primary text
	Tan    lipgloss.Style // secondary text
	Gray   lipgloss.Style // labels
	Dim    lipgloss.Style // log output
	Faint  lipgloss.Style // hints, rails, placeholders
	Sel    lipgloss.Style // the selected row bar
	Badge  lipgloss.Style // the inverse `nfu` tag in the header
	Box    lipgloss.Style // bordered meta/info boxes
}

// NewGruv builds the default-theme manager palette (tests and themeless callers).
func NewGruv() Gruv {
	t, _ := theme.Get("")
	return GruvFromTheme(t)
}

// GruvFor builds the manager palette for the named theme, with the same
// fallback contract as StylesFor.
func GruvFor(name string) Gruv {
	t, err := theme.Get(name)
	if err != nil {
		t, _ = theme.Get("")
	}
	return GruvFromTheme(t)
}

// GruvFromTheme maps a theme onto the manager style set.
func GruvFromTheme(t theme.Theme) Gruv {
	c := func(v string) lipgloss.Style { return lipgloss.NewStyle().Foreground(lipgloss.Color(v)) }
	return Gruv{
		Orange: c(t.Accent),
		Yellow: c(t.Warn),
		Green:  c(t.Good),
		Red:    c(t.Bad),
		Blue:   c(t.Title),
		Aqua:   c(t.AccentAlt),
		Purple: c(t.Heading),
		Fg:     c(t.Value),
		Tan:    c(t.Muted),
		Gray:   c(t.Label),
		Dim:    c(t.Dim),
		Faint:  c(t.Faint),
		Sel:    lipgloss.NewStyle().Background(lipgloss.Color(t.SelBg)).Foreground(lipgloss.Color(t.Value)),
		Badge:  lipgloss.NewStyle().Background(lipgloss.Color(t.Accent)).Foreground(lipgloss.Color(t.BadgeFg)).Bold(true).Padding(0, 1),
		Box:    lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color(t.Border)).Padding(0, 1),
	}
}
