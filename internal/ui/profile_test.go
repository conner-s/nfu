package ui

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/colorprofile"

	"github.com/conner-s/nfu/internal/flake"
	"github.com/conner-s/nfu/internal/theme"
)

// lipgloss v2 renders ANSI unconditionally — it stopped looking at the terminal
// — so the "is stdout a TTY" decision now lives in StylesFromThemeFor. These
// tests pin both halves of it: nothing escapes on a pipe, and a real terminal
// still gets color.
func renderedLines(s Styles) string {
	inputs := []flake.Input{
		{Name: "nixpkgs", Rev: "6bdb38aa582f3ab09bb61a9fddd4cec7e069eee2", LastModified: 1_784_000_000},
		{Name: "mirror", Follows: true},
	}
	res := flake.UpdateResult{Input: "nixpkgs", OldPin: "aaaaaaabbbbbbb", NewPin: "cccccccddddddd", NewMod: 1_784_100_000}

	var b bytes.Buffer
	fmt.Fprintln(&b, RenderInputList(s, inputs, 1_784_170_000))
	fmt.Fprintln(&b, RenderResultRow(s, res, 10, 1_784_170_000))
	fmt.Fprintln(&b, s.Warn.Render("! lock file is dirty"))
	return b.String()
}

func TestStylesNoTTYEmitNoEscapes(t *testing.T) {
	hex, err := theme.Get("gruvbox-dark") // the hex palette, the worst case
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []Profile{colorprofile.NoTTY, colorprofile.ASCII} {
		out := renderedLines(StylesFromThemeFor(hex, p))
		if strings.Contains(out, "\x1b") {
			t.Fatalf("profile %s leaked an escape sequence: %q", p, out)
		}
		if !strings.Contains(out, "nixpkgs") {
			t.Fatalf("profile %s dropped the text as well: %q", p, out)
		}
	}
}

func TestStylesColorTTYEmitsColor(t *testing.T) {
	hex, err := theme.Get("gruvbox-dark")
	if err != nil {
		t.Fatal(err)
	}
	out := renderedLines(StylesFromThemeFor(hex, colorprofile.TrueColor))
	if !strings.Contains(out, "\x1b") {
		t.Fatal("truecolor profile rendered no escape sequences")
	}
	// A 256-color terminal must still get the hex palette, downsampled rather
	// than dropped (README's promise about hex themes).
	out256 := renderedLines(StylesFromThemeFor(hex, colorprofile.ANSI256))
	if strings.Contains(out256, ";2;") {
		t.Fatalf("ANSI256 profile emitted a truecolor sequence: %q", out256)
	}
	if !strings.Contains(out256, "\x1b[38;5;") {
		t.Fatalf("ANSI256 profile emitted no indexed color: %q", out256)
	}
}

// The manager's boxes are layout, not color: their border and padding must
// survive a profile with no color at all, or every pane measures short.
func TestGruvBoxKeepsBorderWithoutColor(t *testing.T) {
	g := GruvFromThemeFor(mustTheme(t), colorprofile.NoTTY)
	out := g.Box.Render("x")
	if strings.Contains(out, "\x1b") {
		t.Fatalf("NoTTY box leaked an escape sequence: %q", out)
	}
	if !strings.Contains(out, "╮") {
		t.Fatalf("NoTTY box lost its border: %q", out)
	}
}

func mustTheme(t *testing.T) theme.Theme {
	t.Helper()
	th, err := theme.Get("")
	if err != nil {
		t.Fatal(err)
	}
	return th
}
