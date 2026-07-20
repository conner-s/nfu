package ui

import (
	"strings"
	"testing"

	"github.com/conner-s/nfu/internal/flake"
	"github.com/conner-s/nfu/internal/theme"
)

func TestShortPin(t *testing.T) {
	cases := map[string]string{
		"6bdb38aa582f3ab09bb61a9fddd4cec7e069eee2":            "6bdb38a",
		"sha256-AkwK4si8cpuXFCrE082x3WhqkIZaUpu2uO/AFiCW0sU=": "AkwK4si8cpuX",
		"":      "—",
		"short": "short",
	}
	for in, want := range cases {
		if got := ShortPin(in); got != want {
			t.Errorf("ShortPin(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRelAge(t *testing.T) {
	now := int64(1_000_000_000)
	cases := []struct {
		unix int64
		want string
	}{
		{0, "—"},
		{now, "just now"},
		{now - 1, "1 second ago"},
		{now - 10, "10 seconds ago"},
		{now - 44, "44 seconds ago"},
		{now - 90, "1 minute ago"},
		{now - 3600, "1 hour ago"},
		{now - 2*24*3600, "2 days ago"},
		{now + 5, "just now"}, // future clamps to just now
	}
	for _, c := range cases {
		if got := RelAge(c.unix, now); got != c.want {
			t.Errorf("RelAge(%d, %d) = %q, want %q", c.unix, now, got, c.want)
		}
	}
}

func TestAbsTime(t *testing.T) {
	if got := AbsTime(0); got != "—" {
		t.Errorf("AbsTime(0) = %q, want —", got)
	}
	// A real timestamp renders as a "YYYY-MM-DD HH:MM" stamp; assert the shape
	// (exact value is local-timezone dependent, so don't pin the clock).
	got := AbsTime(1_784_100_000)
	if len(got) != len("2026-07-15 11:37") || got[4] != '-' || got[7] != '-' || got[10] != ' ' || got[13] != ':' {
		t.Errorf("AbsTime = %q, want a YYYY-MM-DD HH:MM stamp", got)
	}
}

func TestRenderResultsSmoke(t *testing.T) {
	s := NewStyles()
	now := int64(1_784_100_000)
	results := []flake.UpdateResult{
		{Input: "totality", OldPin: "6bdb38aa582f3ab09bb61a9fddd4cec7e069eee2", NewPin: "1122334455667788990011223344556677889900", OldMod: 1784048328, NewMod: 1784099000},
		{Input: "npk", OldPin: "same", NewPin: "same", Unchanged: true},
	}
	out := RenderResults(s, results, now)
	if !strings.Contains(out, "totality") || !strings.Contains(out, "npk") {
		t.Errorf("RenderResults missing inputs:\n%s", out)
	}
	if !strings.Contains(out, "already current") {
		t.Errorf("unchanged input should say 'already current':\n%s", out)
	}
}

// Render through a hex theme so the truecolor path is exercised, and through
// every registered theme so a palette with a broken slot can't slip in.
func TestRenderResultsThemedSmoke(t *testing.T) {
	results := []flake.UpdateResult{
		{Input: "totality", OldPin: "6bdb38aa582f3ab09bb61a9fddd4cec7e069eee2", NewPin: "1122334455667788990011223344556677889900", OldMod: 1784048328, NewMod: 1784099000},
	}
	for _, name := range theme.Names() {
		s := StylesFor(name)
		out := RenderResults(s, results, int64(1_784_100_000))
		if !strings.Contains(out, "totality") {
			t.Errorf("theme %q: RenderResults missing input:\n%s", name, out)
		}
	}
}

// An unknown name (impossible via config, which validates at load) must fall
// back to the default palette rather than panic or render blank.
func TestStylesForFallback(t *testing.T) {
	s := StylesFor("no-such-theme")
	if got, want := s.Title.Render("x"), NewStyles().Title.Render("x"); got != want {
		t.Errorf("fallback Title = %q, want default %q", got, want)
	}
}

func TestRenderInputListSmoke(t *testing.T) {
	s := NewStyles()
	inputs := []flake.Input{
		{Name: "nixpkgs", Type: "github", Ref: "nixos-26.05", Rev: "1111111111111111111111111111111111111111", LastModified: 1784000000},
		{Name: "mirror", Follows: true},
	}
	out := RenderInputList(s, inputs, 1784100000)
	if !strings.Contains(out, "nixpkgs") {
		t.Errorf("missing nixpkgs:\n%s", out)
	}
	if !strings.Contains(out, "follows") {
		t.Errorf("follows alias should be labeled:\n%s", out)
	}
}
