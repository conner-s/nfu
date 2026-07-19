// Package ui renders nfu's terminal output. Styling mirrors npk's palette so
// the two tools feel like one family; lipgloss auto-strips color when stdout is
// not a TTY, so piped and --json output stays clean.
package ui

import "github.com/charmbracelet/lipgloss"

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

// NewStyles builds the shared palette (npk's colors).
func NewStyles() Styles {
	return Styles{
		Title: lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12")),
		Label: lipgloss.NewStyle().Foreground(lipgloss.Color("244")),
		Value: lipgloss.NewStyle().Foreground(lipgloss.Color("252")),
		Dim:   lipgloss.NewStyle().Foreground(lipgloss.Color("240")),
		Good:  lipgloss.NewStyle().Foreground(lipgloss.Color("10")),
		Warn:  lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("11")),
		Bad:   lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("9")),
		Help:  lipgloss.NewStyle().Foreground(lipgloss.Color("241")),
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

// Gruv is the gruvbox palette the full-screen project manager paints with. The
// manager owns the whole alt-screen (panes, selection bars, a log tail), so it
// carries a richer palette than the line-oriented Styles above; lipgloss
// downsamples the hex values on terminals without truecolor.
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

// NewGruv builds the manager palette (colors from the Claude Design prototype).
func NewGruv() Gruv {
	c := func(hex string) lipgloss.Style { return lipgloss.NewStyle().Foreground(lipgloss.Color(hex)) }
	return Gruv{
		Orange: c("#fe8019"),
		Yellow: c("#fabd2f"),
		Green:  c("#b8bb26"),
		Red:    c("#fb4934"),
		Blue:   c("#83a598"),
		Aqua:   c("#8ec07c"),
		Purple: c("#d3869b"),
		Fg:     c("#ebdbb2"),
		Tan:    c("#bdae93"),
		Gray:   c("#928374"),
		Dim:    c("#7c6f64"),
		Faint:  c("#665c54"),
		Sel:    lipgloss.NewStyle().Background(lipgloss.Color("#504945")).Foreground(lipgloss.Color("#ebdbb2")),
		Badge:  lipgloss.NewStyle().Background(lipgloss.Color("#fe8019")).Foreground(lipgloss.Color("#282828")).Bold(true).Padding(0, 1),
		Box:    lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("#3c3836")).Padding(0, 1),
	}
}
