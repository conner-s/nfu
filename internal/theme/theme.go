// Package theme defines nfu's named color palettes.
//
// A Theme is a flat set of semantic color slots (never widget names); the ui
// package maps one theme onto both of its style sets — the line-oriented
// Styles and the full-screen manager's Gruv. The registry is keyed by the
// `theme` config value, validated at config load so a typo fails with the
// valid set instead of rendering a silently wrong palette.
package theme

import (
	"fmt"
	"sort"
	"strings"
)

// Theme is one palette. Values are lipgloss color strings: ANSI-256 codes
// ("205") or hex ("#fe8019"). Downsampling to what the terminal can show is
// the ui package's job — it resolves a color profile once and converts every
// slot through it (see ui.StylesFromThemeFor).
type Theme struct {
	Name string

	// Text ramp, brightest to faintest.
	Title   string // screen titles; remote/update accents (the blue family)
	Heading string // manager section titles (the purple family)
	Value   string // primary text
	Muted   string // secondary text
	Label   string // field labels, table headers
	Help    string // key-hint lines
	Dim     string // tertiary text, log output
	Faint   string // hints, rails, placeholders

	// Status.
	Good string // success / additions
	Warn string // warnings / in-flight state
	Bad  string // errors / removals

	// Accents.
	Accent    string // selection marker, badge, deploy steps (nfu's orange)
	AccentAlt string // local targets, switch steps, toml keys (the aqua family)

	// Chrome.
	SelBg   string // selected-row bar background
	BadgeFg string // text atop Accent (the inverse header badge)
	Border  string // box borders
}

// DefaultName is the theme used when the config carries no `theme` key. It is
// built from ANSI palette codes so an unconfigured nfu inherits the user's
// terminal scheme — that is a feature, not a fallback.
const DefaultName = "default"

// Keep in sync with the `theme` enum in modules/home-manager.nix.
var themes = map[string]Theme{
	DefaultName: {
		Name:  DefaultName,
		Title: "12", Heading: "13", Value: "252", Muted: "250",
		Label: "244", Help: "241", Dim: "240", Faint: "238",
		Good: "10", Warn: "11", Bad: "9",
		Accent: "208", AccentAlt: "14",
		SelBg: "237", BadgeFg: "0", Border: "238",
	},
	"gruvbox-dark": {
		Name:  "gruvbox-dark",
		Title: "#83a598", Heading: "#d3869b", Value: "#ebdbb2", Muted: "#bdae93",
		Label: "#928374", Help: "#928374", Dim: "#7c6f64", Faint: "#665c54",
		Good: "#b8bb26", Warn: "#fabd2f", Bad: "#fb4934",
		Accent: "#fe8019", AccentAlt: "#8ec07c",
		SelBg: "#504945", BadgeFg: "#282828", Border: "#3c3836",
	},
	"gruvbox-light": {
		Name:  "gruvbox-light",
		Title: "#076678", Heading: "#8f3f71", Value: "#3c3836", Muted: "#665c54",
		Label: "#7c6f64", Help: "#928374", Dim: "#a89984", Faint: "#bdae93",
		Good: "#79740e", Warn: "#b57614", Bad: "#9d0006",
		Accent: "#af3a03", AccentAlt: "#427b58",
		SelBg: "#d5c4a1", BadgeFg: "#fbf1c7", Border: "#bdae93",
	},
	"muted": {
		Name:  "muted",
		Title: "#8ab4d8", Heading: "#7e8aa6", Value: "#c6cad1", Muted: "#9aa2ad",
		Label: "#7e8aa6", Help: "#565e68", Dim: "#565e68", Faint: "#454c55",
		Good: "#8fae76", Warn: "#cdb383", Bad: "#c07a72",
		Accent: "#8ab4d8", AccentAlt: "#7ba99a",
		SelBg: "#343c46", BadgeFg: "#1c2126", Border: "#4a5560",
	},
}

// Names returns every registered theme name, sorted.
func Names() []string {
	out := make([]string, 0, len(themes))
	for n := range themes {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// Get returns the named theme; "" means the default. Unknown names error with
// the valid set so config load can surface a helpful message.
func Get(name string) (Theme, error) {
	if name == "" {
		name = DefaultName
	}
	t, ok := themes[name]
	if !ok {
		return Theme{}, fmt.Errorf("unknown theme %q (have: %s)", name, strings.Join(Names(), ", "))
	}
	return t, nil
}
