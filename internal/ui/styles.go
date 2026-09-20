// Package ui renders nfu's terminal output. Colors come exclusively from the
// theme registry (internal/theme) — the `theme` config key picks the palette,
// and every style set is built for a detected color profile, so piped, NO_COLOR
// and --json output stays clean while a real terminal gets the palette
// downsampled to what it can show.
package ui

import (
	"image/color"
	"os"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"

	"github.com/conner-s/nfu/internal/theme"
)

// Profile is the color capability of the stream a style set renders to.
// lipgloss v2 renders ANSI unconditionally — it no longer looks at the
// terminal — so nfu resolves the profile once, here, and bakes it into the
// styles. Everything downstream stays a plain fmt.Fprint of a rendered string.
type Profile = colorprofile.Profile

// DetectProfile reports stdout's color profile: NoTTY when stdout is a pipe or
// a file, ASCII when NO_COLOR or a dumb terminal says "no color", otherwise the
// richest of what $TERM, terminfo and tmux support.
func DetectProfile() Profile {
	return colorprofile.Detect(os.Stdout, os.Environ())
}

// colorer maps theme color strings onto a profile: hex downsamples to 256 or
// 16 colors, and a profile with no color at all yields nil (an unset
// foreground), which renders no escape sequence.
func colorer(p Profile) func(string) color.Color {
	if p <= colorprofile.ASCII {
		return func(string) color.Color { return nil }
	}
	return func(v string) color.Color { return p.Convert(lipgloss.Color(v)) }
}

// attrs reports whether the profile renders SGR attributes (color, bold) at
// all. Structural attributes — borders, padding, width — always render; that
// is the split lipgloss v1 made for us automatically.
func attrs(p Profile) bool { return p > colorprofile.ASCII }

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

// StylesFromTheme maps a theme onto the line-oriented style set for stdout's
// detected profile.
func StylesFromTheme(t theme.Theme) Styles {
	return StylesFromThemeFor(t, DetectProfile())
}

// StylesFromThemeFor is StylesFromTheme against an explicit profile. Emphasis
// (bold) lives here on purpose: themes recolor, they do not relayout. A
// profile with no color drops it too, which is what lipgloss v1 did on a pipe.
func StylesFromThemeFor(t theme.Theme, p Profile) Styles {
	if !attrs(p) {
		plain := lipgloss.NewStyle()
		return Styles{
			Title: plain, Label: plain, Value: plain, Dim: plain,
			Good: plain, Warn: plain, Bad: plain, Help: plain,
		}
	}
	c := colorer(p)
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

// GruvFromTheme maps a theme onto the manager style set for stdout's detected
// profile.
func GruvFromTheme(t theme.Theme) Gruv {
	return GruvFromThemeFor(t, DetectProfile())
}

// GruvFromThemeFor is GruvFromTheme against an explicit profile. Box keeps its
// border and padding at every profile — those are layout, not color, and the
// views measure them.
func GruvFromThemeFor(t theme.Theme, p Profile) Gruv {
	box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1)
	badge := lipgloss.NewStyle().Padding(0, 1)
	if !attrs(p) {
		plain := lipgloss.NewStyle()
		return Gruv{
			Orange: plain, Yellow: plain, Green: plain, Red: plain,
			Blue: plain, Aqua: plain, Purple: plain, Fg: plain,
			Tan: plain, Gray: plain, Dim: plain, Faint: plain,
			Sel: plain, Badge: badge, Box: box,
		}
	}
	col := colorer(p)
	c := func(v string) lipgloss.Style { return lipgloss.NewStyle().Foreground(col(v)) }
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
		Sel:    lipgloss.NewStyle().Background(col(t.SelBg)).Foreground(col(t.Value)),
		Badge:  badge.Background(col(t.Accent)).Foreground(col(t.BadgeFg)).Bold(true),
		Box:    box.BorderForeground(col(t.Border)),
	}
}
