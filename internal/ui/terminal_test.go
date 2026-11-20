package ui

import (
	"errors"
	"os"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/conner-s/termsafe"

	"github.com/conner-s/nfu/internal/flake"
)

var eAcute = string(rune(0x00E9)) // e with an acute accent

func TestChooseTerminal(t *testing.T) {
	for charset, want := range map[string]termsafe.Encoding{
		"utf8": termsafe.EncodingUTF8, "UTF-8": termsafe.EncodingUTF8, "utf-8": termsafe.EncodingUTF8,
		"ascii": termsafe.EncodingNotUTF8, "ASCII": termsafe.EncodingNotUTF8,
	} {
		got, err := ChooseTerminal(charset, os.Stdout)
		if err != nil || got.Encoding != want || got.Reason.IsEmpty() {
			t.Errorf("ChooseTerminal(%q) = %v (%q), %v; want %v with a reason", charset, got.Encoding, got.Reason, err, want)
		}
	}
	// auto is the check's own answer, whatever that is here.
	for _, auto := range []string{"", "auto"} {
		got, err := ChooseTerminal(auto, os.Stdout)
		if err != nil || got.Encoding != termsafe.CheckTerminal(os.Stdout).Encoding {
			t.Errorf("ChooseTerminal(%q) = %v, %v; want what CheckTerminal says", auto, got.Encoding, err)
		}
	}
	if _, err := ChooseTerminal("latin1", os.Stdout); err == nil || !strings.Contains(err.Error(), "auto, utf8 or ascii") {
		t.Errorf("ChooseTerminal(latin1) = %v, want an error naming the three values", err)
	}
}

// Until a verdict is set nothing is known about the terminal, and that means
// ASCII.
func TestNoVerdictMeansASCII(t *testing.T) {
	defer SetTerminal(termsafe.Terminal{})()
	if UTF8() || Name("caf"+eAcute).String() != "caf<U+00E9>" || Own("•") != "*" {
		t.Errorf("with no verdict: UTF8 %v, Name %q, Own %q", UTF8(), Name("caf"+eAcute), Own("•"))
	}
}

func TestTerminalWarning(t *testing.T) {
	if w := TerminalWarning(); !w.IsEmpty() {
		t.Errorf("a UTF-8 terminal: %q, want no warning", w)
	}
	defer SetTerminal(notUTF8Terminal)()
	const want = "WARNING: this terminal is not known to decode UTF-8 (LANG=C is the C locale, whose characters are ASCII). " +
		"nfu draws ASCII only and shows any other character by its code point, such as <U+00E9>. " +
		`If this terminal does decode UTF-8, pass --charset=utf8 or set charset = "utf8" in the config.`
	if got := TerminalWarning().String(); got != want {
		t.Errorf("TerminalWarning = %q\n            want %q", got, want)
	}
	if p := termsafe.ASCIIProblem(TerminalWarning().String()); p != "" {
		t.Error(p)
	}
	for _, l := range TerminalBanner(40) {
		if len(l) > 40 {
			t.Errorf("a banner line is wider than 40: %q", l)
		}
	}
}

// On a terminal that is not UTF-8 every conversion gives the ASCII form of
// outside text, and nfu's own glyphs their stand-ins.
func TestConversionsFitTheTerminal(t *testing.T) {
	defer SetTerminal(notUTF8Terminal)()
	name := "caf" + eAcute
	lines, _ := ErrLines(errors.New(name + "\n" + name))
	for what, got := range map[string]string{
		"Name":      Name(name).String(),
		"Line":      Line(name).String(),
		"Names":     Names([]string{name, name}, " + ").String(),
		"Words":     Words("✓ done → next").String(),
		"Linef":     Linef("✗ %s: %s", name, errors.New(name)).String(),
		"Pin":       Pin(name).String(),
		"empty pin": Pin("").String(),
		"ErrLine":   ErrLine(errors.New(name)).String(),
		"ErrLines":  lines[1].String(),
		"note":      HostNameNote(name).String(),
		"warning":   ArgWarning("An argument", name+"\x07").String(),
	} {
		if p := termsafe.ASCIIProblem(got); p != "" {
			t.Errorf("%s = %q: %s", what, got, p)
		}
	}
	for got, want := range map[string]string{
		Name(name).String():                        "caf<U+00E9>",
		Names([]string{name, "b"}, " + ").String(): "caf<U+00E9> + b",
		Words("✓ done → next").String():            "+ done > next",
		Linef("✗ %s", name).String():               "x caf<U+00E9>",
		Pin("").String():                           "-",
	} {
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	}
	// Clipping comes after fitting: sixty-four characters of what is drawn.
	long := ErrLine(errors.New(strings.Repeat(eAcute, 30))).String()
	if len(long) != 64 || !strings.HasSuffix(long, "...") {
		t.Errorf("a clipped error is %d characters: %q", len(long), long)
	}
	// A mixed-script word is found before the ASCII form is taken, and then
	// shown in it: on this terminal the look-alike cannot be drawn.
	cyrA := string(rune(0x0430))
	if got := HostNameNote("F" + cyrA + "bricator").String(); got != `"F<U+0430>bricator" mixes Cyrillic and Latin letters (U+0430 is Cyrillic)` {
		t.Errorf("note = %q", got)
	}
}

// Own leaves everything alone on a UTF-8 terminal. On any other, every glyph
// nfu and its libraries draw has a stand-in one column wide, and what has
// none is written by its code point.
func TestOwn(t *testing.T) {
	const drawn = "✓ ok · 3 of 5 → next…"
	if got := Own(drawn); got != drawn {
		t.Errorf("on a UTF-8 terminal Own changed %q to %q", drawn, got)
	}
	defer SetTerminal(notUTF8Terminal)()
	if got := Own(drawn); got != "+ ok - 3 of 5 > next." {
		t.Errorf("Own = %q", got)
	}
	for r, stand := range standIns {
		if len(stand) != 1 || stand[0] >= utf8.RuneSelf {
			t.Errorf("the stand-in for U+%04X is %q, want one ASCII character", r, stand)
		}
	}
	if got := Own("caf" + eAcute + "\xff"); got != "caf<U+00E9>?" {
		t.Errorf("Own of text with no stand-in = %q", got)
	}
	// The escape sequences of a style are ASCII and stay.
	styled := "\x1b[1;32m✓\x1b[0m"
	if got := Own(styled); got != "\x1b[1;32m+\x1b[0m" {
		t.Errorf("Own of styled text = %q", got)
	}
}

// The render helpers under the same terminal: rows hold only ASCII once they
// have been through Own, as every place that prints them puts them.
func TestRenderHelpers_HostileText_NotUTF8(t *testing.T) {
	defer SetTerminal(notUTF8Terminal)()
	s := NewStyles()
	now := int64(1_784_100_000)
	for _, h := range termsafe.Samples() {
		text := h.Text
		inputs := InputRows([]flake.Input{
			{Name: text, Type: text, Ref: text, Rev: text, LastModified: now - 60},
			{Name: "alias" + text, Follows: true},
		})
		moved := NewResultRow(flake.UpdateResult{Input: text, OldPin: text, NewPin: "new" + text})
		for name, out := range map[string]string{
			"RenderInputList":    RenderInputList(s, inputs, now),
			"RenderResults":      RenderResults(s, []ResultRow{moved}, now),
			"RenderUpdatingText": RenderUpdatingText(s, Name(text), 8),
			"RenderFailedRow":    RenderFailedRow(s, Name(text), 8),
			"TitleLine":          TitleLine(s, Line(text), Line("/home/user/"+text)),
		} {
			out = Own(out)
			if p := termsafe.ViewProblem(out); p != "" {
				t.Errorf("%s, %s: %s:\n%q", name, h.Name, p, out)
			}
			if p := termsafe.ASCIIProblem(out); p != "" {
				t.Errorf("%s, %s: %s:\n%q", name, h.Name, p, out)
			}
		}
	}
}
