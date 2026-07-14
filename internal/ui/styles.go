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
