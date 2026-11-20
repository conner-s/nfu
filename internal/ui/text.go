package ui

import (
	"fmt"
	"strings"

	"github.com/conner-s/termsafe"

	"github.com/conner-s/nfu/internal/flake"
)

// This file is where text nfu did not write becomes text it may draw. A
// flake.lock field, a name or path from a TOML file, a file name, what nix or
// git said in an error, an argument: each is cleaned by one of the functions
// below (or by proc.RunLines, for the output of nh and deploy), and everything
// that draws takes the termsafe.Text they return. docs/untrusted-text.md
// follows every such value from where it comes in to where it is shown.
//
// The policy is chosen here, per kind of text. Names, pins, paths, messages
// and errors are text a person acts on, so a bidirectional control or an
// invisible character in them is shown as a marker such as <U+202E>
// (termsafe.Mark). The output of nh and deploy is a log that is watched going
// by, so there they are removed (termsafe.Drop, in proc.RunLines).
//
// Each function ends in Fit (terminal.go): on a terminal that is not known to
// decode UTF-8 the text comes out in its ASCII form, every other character
// shown by its code point. That is done here, before a view pads or clips, so
// that one character is one column there. nfu's own words in a Words or Linef
// get their ASCII stand-ins instead (Own).
//
// A Text is for the screen only. What nfu compares, hands to nix, git or
// deploy, or writes to a file stays the raw string.

// Name cleans a name a person acts on: a flake input, a deploy host, a
// project.
func Name(raw string) termsafe.Text {
	return Fit(termsafe.Mark.Line(raw))
}

// Line cleans any other text that is drawn on one line: a path, a pin, a ref,
// a description, a commit subject, a warning that quotes git.
func Line(raw string) termsafe.Text {
	return Fit(termsafe.Mark.Line(raw))
}

// Names cleans a list and joins it. Each element is cleaned by itself first:
// a control string without a terminator runs to the end of its line, so
// cleaning the joined list would let one name swallow the names after it.
func Names(raw []string, sep string) termsafe.Text {
	parts := make([]string, len(raw))
	for i, r := range raw {
		parts[i] = Fit(termsafe.Mark.Line(r)).String()
	}
	return termsafe.Mark.Line(Own(strings.Join(parts, sep)))
}

// Words is for text nfu wrote itself, where a Text is wanted. It cleans all
// the same, so handing it outside text by mistake is not a hole.
func Words(own string) termsafe.Text {
	return termsafe.Mark.Line(Own(own))
}

// Linef formats one line from nfu's own words and outside values. Every
// string and error among args is cleaned and fitted by itself before it is
// put in, for the reason given at Names; the format's own glyphs get their
// stand-ins, and the result is cleaned again as a whole.
func Linef(format string, args ...any) termsafe.Text {
	cleaned := make([]any, len(args))
	for i, a := range args {
		switch v := a.(type) {
		case string:
			cleaned[i] = Fit(termsafe.Mark.Line(v)).String()
		case error:
			cleaned[i] = Fit(termsafe.Mark.Line(v.Error())).String()
		case termsafe.Text:
			cleaned[i] = v.String()
		default:
			cleaned[i] = a
		}
	}
	return termsafe.Mark.Line(Own(fmt.Sprintf(format, cleaned...)))
}

// Pin cleans a locked pin and abbreviates it as ShortPin does. Cleaning and
// fitting come first, so that the abbreviation is taken from what is left to
// read and not from the head of a control string. The "—" of an empty pin is
// nfu's own. Cutting cleaned text between two
// characters leaves it clean; it is wrapped again to be a Text.
func Pin(raw string) termsafe.Text {
	return termsafe.Mark.Line(Own(ShortPin(Fit(termsafe.Mark.Line(raw)).String())))
}

// Err cleans an error for printing in full. It keeps its lines: an error
// from nix carries nix's whole stderr. The report says what was taken out.
func Err(err error) (termsafe.Text, termsafe.Report) {
	text, rep := termsafe.Mark.CleanReport(err.Error())
	return Fit(text), rep
}

// ErrLines is Err with the text split into its lines, for a pane that draws
// and clips one line at a time.
func ErrLines(err error) ([]termsafe.Text, termsafe.Report) {
	text, rep := Err(err)
	parts := strings.Split(text.String(), "\n")
	lines := make([]termsafe.Text, len(parts))
	for i, p := range parts {
		lines[i] = termsafe.Mark.Line(p)
	}
	return lines, rep
}

// errLineMax is the length ErrLine clips to: the full stderr of nix would
// overflow a table row or the footer.
const errLineMax = 64

// ErrLine condenses an error to its first line, trimmed and clipped, for a
// row marker or a toast.
func ErrLine(err error) termsafe.Text {
	s := err.Error()
	// A control sequence never reaches past the end of its line, so taking
	// the first line before cleaning gives what cleaning first would.
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = s[:i]
	}
	return Fit(termsafe.Mark.Line(s).TrimSpace()).Truncate(errLineMax)
}

// InputRow is a flake input as it is drawn, in `nfu list`, in the picker and
// in the project form. It is built from a flake.Input, which stays the value
// nfu compares and passes to nix.
type InputRow struct {
	Name termsafe.Text
	Type termsafe.Text
	Ref  termsafe.Text
	Pin  termsafe.Text // abbreviated
	// Note says why the name deserves a second look (see InputNameNote).
	// It is empty for an ordinary name.
	Note         termsafe.Text
	LastModified int64
	Follows      bool
}

// NewInputRow cleans one input for display.
func NewInputRow(in flake.Input) InputRow {
	return InputRow{
		Name:         Name(in.Name),
		Type:         Line(in.Type),
		Ref:          Line(in.Ref),
		Pin:          Pin(in.Pin()),
		Note:         InputNameNote(in.Name),
		LastModified: in.LastModified,
		Follows:      in.Follows,
	}
}

// InputRows cleans a list of inputs for display.
func InputRows(inputs []flake.Input) []InputRow {
	rows := make([]InputRow, len(inputs))
	for i, in := range inputs {
		rows[i] = NewInputRow(in)
	}
	return rows
}

// ResultRow is the outcome of updating one input, as it is drawn.
type ResultRow struct {
	Input     termsafe.Text
	OldPin    termsafe.Text // abbreviated
	NewPin    termsafe.Text // abbreviated
	OldMod    int64
	NewMod    int64
	Unchanged bool
}

// NewResultRow cleans one update result for display.
func NewResultRow(r flake.UpdateResult) ResultRow {
	return ResultRow{
		Input:     Name(r.Input),
		OldPin:    Pin(r.OldPin),
		NewPin:    Pin(r.NewPin),
		OldMod:    r.OldMod,
		NewMod:    r.NewMod,
		Unchanged: r.Unchanged,
	}
}

// ResultRows cleans a list of update results for display.
func ResultRows(results []flake.UpdateResult) []ResultRow {
	rows := make([]ResultRow, len(results))
	for i, r := range results {
		rows[i] = NewResultRow(r)
	}
	return rows
}
