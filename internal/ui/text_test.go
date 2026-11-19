package ui

import (
	"errors"
	"strings"
	"testing"

	"forgejo.kube.internal.conn3r.xyz/connerlab/termsafe"

	"github.com/conner-s/nfu/internal/flake"
)

// The characters these tests need, built from their code points so that none
// hides in the source.
var (
	rlo  = string(rune(0x202E)) // right-to-left override
	zwsp = string(rune(0x200B)) // zero width space
	cyrO = string(rune(0x043E)) // Cyrillic o
	cyrA = string(rune(0x0430)) // Cyrillic a
)

// A control string without a terminator runs to the end of its line. In a
// list drawn on one line that must not hide the names after it.
func TestNamesCleansEachElementByItself(t *testing.T) {
	got := Names([]string{"first\x1b]0;never ended", "second", "third"}, ", ").String()
	if got != "first, second, third" {
		t.Errorf("Names = %q, want every name still there", got)
	}
	got = Linef("update %s then %s", "a\x1bPopen", "b").String()
	if got != "update a then b" {
		t.Errorf("Linef = %q, want the words after the first value still there", got)
	}
}

func TestLinefTakesTextAndErrors(t *testing.T) {
	got := Linef("✗ %s%s (%d)", "save: ", ErrLine(errors.New("no"+rlo+"pe")), 3).String()
	if got != "✗ save: no<U+202E>pe (3)" {
		t.Errorf("Linef = %q", got)
	}
	if got := Linef("! %s", errors.New("bad\x07 thing")).String(); got != "! bad thing" {
		t.Errorf("Linef with an error = %q", got)
	}
}

// Names, pins and messages are marked, not silently altered: a person acts on
// them.
func TestPolicyMarksWhatAPersonActsOn(t *testing.T) {
	for name, got := range map[string]termsafe.Text{
		"Name": Name("nix" + rlo + "pkgs"),
		"Line": Line("nix" + rlo + "pkgs"),
	} {
		if got.String() != "nix<U+202E>pkgs" {
			t.Errorf("%s = %q, want the override shown as a marker", name, got)
		}
	}
	// A pin is clipped to twelve characters, marker included.
	if got := Pin("nix" + rlo + "pkgs").String(); got != "nix<U+202E>p" {
		t.Errorf("Pin = %q, want the override shown as a marker", got)
	}
}

func TestPin(t *testing.T) {
	cases := map[string]string{
		"6bdb38aa582f3ab09bb61a9fddd4cec7e069eee2":            "6bdb38a",
		"sha256-AkwK4si8cpuXFCrE082x3WhqkIZaUpu2uO/AFiCW0sU=": "AkwK4si8cpuX",
		"": "—",
		// Cleaned first: the abbreviation is of what is left to read.
		"\x1b]8;;https://example/\x1b\\6bdb38aa582f3ab09bb61a9fddd4cec7e069eee2": "6bdb38a",
		"\x1b[2J" + strings.Repeat("→", 40):                                      strings.Repeat("→", 12),
	}
	for in, want := range cases {
		if got := Pin(in).String(); got != want {
			t.Errorf("Pin(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestErrKeepsLinesAndReports(t *testing.T) {
	err := errors.New("nix failed: exit status 1: error: first\x1b[31m red\x1b[0m\n       second \x1bPcut here")
	text, rep := Err(err)
	if text.String() != "nix failed: exit status 1: error: first red\n       second " {
		t.Errorf("Err = %q", text)
	}
	if rep.Sequences != 2 || rep.Unterminated != 1 {
		t.Errorf("report = %+v, want 2 sequences and 1 unterminated", rep)
	}
	lines, _ := ErrLines(err)
	if len(lines) != 2 || lines[0].String() != "nix failed: exit status 1: error: first red" {
		t.Errorf("ErrLines = %q", lines)
	}
}

func TestCutNote(t *testing.T) {
	if !CutNote(0).IsEmpty() || !CutNote(-1).IsEmpty() {
		t.Error("CutNote gives a note when nothing was cut")
	}
	if got := CutNote(1).String(); got != "info: an unterminated control string was cut from the text above, with the rest of its line" {
		t.Errorf("CutNote(1) = %q", got)
	}
	if got := CutNote(3).String(); got != "info: 3 unterminated control strings were cut from the text above, each with the rest of its line" {
		t.Errorf("CutNote(3) = %q", got)
	}
}

func TestArgWarning(t *testing.T) {
	for _, clean := range []string{"nixpkgs", "home-manager", "/home/user/flake", "日本語", ""} {
		if w := ArgWarning("An argument", clean); !w.IsEmpty() {
			t.Errorf("ArgWarning(%q) = %q, want none", clean, w)
		}
	}
	got := ArgWarning("An argument", "nix"+rlo+"pkgs\x1b[2J").String()
	want := `WARNING: An argument holds 1 control sequence and 1 bidirectional control. ` +
		`It is shown as "nix<U+202E>pkgs" and used as it was given. ` +
		`Text like that can make one name pass for another: check it before you go on.`
	if got != want {
		t.Errorf("ArgWarning = %q\n       want %q", got, want)
	}
	// The warning is nfu's words and the cleaned text: nothing that was
	// removed, not even as plain text.
	w := ArgWarning("The value of --flake", "/flake\x1b]52;c;c2VjcmV0\x07").String()
	for _, removed := range []string{"\x1b", "c2VjcmV0", "52;c"} {
		if strings.Contains(w, removed) {
			t.Errorf("the warning draws %q, which was removed: %q", removed, w)
		}
	}
}

func TestTyped(t *testing.T) {
	for _, clean := range []string{"my-project", "--magic-rollback false", "Fabricator", ""} {
		cleaned, warning := Typed("The pasted text", clean)
		if cleaned != clean || !warning.IsEmpty() {
			t.Errorf("Typed(%q) = %q with warning %q, want it unchanged and no warning", clean, cleaned, warning)
		}
	}
	cleaned, warning := Typed("The pasted text", "fab"+zwsp+"ricator"+rlo+"\x1b[2J")
	if cleaned != "fabricator" {
		t.Errorf("cleaned = %q, want fabricator: a marker would become part of the name", cleaned)
	}
	want := "WARNING: The pasted text held 1 control sequence, 1 bidirectional control and 1 invisible character. " +
		"nfu removed that and uses the cleaned text. Input like that may be malicious: check it before you go on."
	if warning.String() != want {
		t.Errorf("warning = %q\n     want %q", warning, want)
	}
}

// The rule for a flake input's name is nix's: ASCII letters, digits, "_" and
// "-", starting with a letter.
func TestInputNameNote(t *testing.T) {
	for _, plain := range []string{"nixpkgs", "home-manager", "nixpkgs_2", "A", "flake-utils"} {
		if n := InputNameNote(plain); !n.IsEmpty() {
			t.Errorf("InputNameNote(%q) = %q, want none", plain, n)
		}
	}
	const rule = "nix updates only names of ASCII letters, digits, _ and -"
	cases := map[string]string{
		// A look-alike letter among Latin ones: the mixed-script check sees it.
		"nixp" + cyrA + "kgs": `"nixp` + cyrA + `kgs" mixes Cyrillic and Latin letters (U+0430 is Cyrillic); ` + rule,
		// A name wholly in another script: only the ASCII rule sees it.
		cyrO + cyrA + cyrO:   rule,
		"nix" + rlo + "pkgs": "holds 1 bidirectional control; " + rule,
		"with space":         rule,
		"2fast":              rule,
		"":                   rule,
	}
	for name, want := range cases {
		if got := InputNameNote(name).String(); got != want {
			t.Errorf("InputNameNote(%q) = %q\n   want %q", name, got, want)
		}
	}
}

// A deploy host is a flake attribute and may be any string: mixed scripts and
// non-ASCII are noted, nothing is required.
func TestHostNameNote(t *testing.T) {
	for _, plain := range []string{"Fabricator", "edge-01", "host.example", "with space", ""} {
		if n := HostNameNote(plain); !n.IsEmpty() {
			t.Errorf("HostNameNote(%q) = %q, want none", plain, n)
		}
	}
	cases := map[string]string{
		"F" + cyrA + "bricator":  `"F` + cyrA + `bricator" mixes Cyrillic and Latin letters (U+0430 is Cyrillic)`,
		cyrO + cyrA + cyrO:       "not plain ASCII",
		"fab" + zwsp + "ricator": "holds 1 invisible character",
		"fab\x1b[2Kricator":      "holds 1 control sequence",
	}
	for name, want := range cases {
		if got := HostNameNote(name).String(); got != want {
			t.Errorf("HostNameNote(%q) = %q\n   want %q", name, got, want)
		}
	}
}

func TestRenderInputListShowsTheNote(t *testing.T) {
	rows := InputRows([]flake.Input{
		{Name: "nixpkgs", Type: "github", Rev: strings.Repeat("a", 40)},
		{Name: "nixp" + cyrA + "kgs", Type: "github", Rev: strings.Repeat("b", 40)},
	})
	lines := strings.Split(strings.TrimRight(RenderInputList(NewStyles(), rows, 1_784_100_000), "\n"), "\n")
	if strings.Contains(lines[0], "!") {
		t.Errorf("an ordinary name carries a note: %q", lines[0])
	}
	if !strings.Contains(lines[1], "! ") || !strings.Contains(lines[1], "mixes Cyrillic and Latin letters") {
		t.Errorf("a look-alike name carries no note: %q", lines[1])
	}
}

// Every sample of the termsafe kit through every helper that renders a row
// for `nfu list`, `nfu <input>` and the picker's last frame.
func TestRenderHelpers_HostileText(t *testing.T) {
	s := NewStyles()
	now := int64(1_784_100_000)
	for _, h := range termsafe.Samples() {
		text := h.Text
		inputs := InputRows([]flake.Input{
			{Name: text, Type: text, Ref: text, Rev: text, LastModified: now - 60},
			{Name: "hash" + text, Type: "path", NarHash: "sha256-" + text},
			{Name: "alias" + text, Follows: true},
		})
		moved := NewResultRow(flake.UpdateResult{Input: text, OldPin: text, NewPin: "new" + text})
		same := NewResultRow(flake.UpdateResult{Input: text, OldPin: text, NewPin: text, Unchanged: true})
		for name, out := range map[string]string{
			"RenderInputList":    RenderInputList(s, inputs, now),
			"RenderResults":      RenderResults(s, []ResultRow{moved, same}, now),
			"RenderUpdatingText": RenderUpdatingText(s, Name(text), 8),
			"RenderFailedRow":    RenderFailedRow(s, Name(text), 8),
			"TitleLine":          TitleLine(s, Line(text), Line("/home/user/"+text)),
			"ArgWarning":         ArgWarning("An argument", text).String(),
		} {
			if p := termsafe.ViewProblem(out); p != "" {
				t.Errorf("%s, %s: %s:\n%q", name, h.Name, p, out)
			}
		}
	}
}
