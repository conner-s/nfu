package ui

import (
	"fmt"
	"strings"

	"forgejo.kube.internal.conn3r.xyz/connerlab/termsafe"
)

// This file words what nfu tells the user when cleaning changed something, or
// when a name is not what it looks like. The CLI and the TUIs both use it, so
// they say the same thing. A notice is nfu's own words and counts: it never
// draws what was removed.

// CutNote is the note for text that held control strings with no terminator.
// Cleaning cut each one at the end of its line, and whatever stood between
// the string's start and that point went with it; that text may have been
// meant to be read, so nfu says so. n is termsafe.Report.Unterminated; the
// note is empty when it is zero.
func CutNote(n int) termsafe.Text {
	switch {
	case n <= 0:
		return termsafe.Text{}
	case n == 1:
		return Words("info: an unterminated control string was cut from the text above, with the rest of its line")
	}
	return Words(fmt.Sprintf("info: %d unterminated control strings were cut from the text above, each with the rest of its line", n))
}

// ArgWarning returns a warning when something the user passed on the command
// line holds what cleaning removes or marks, and the empty Text when it does
// not. what names it and starts the sentence: "An argument", "The value of
// --flake".
//
// nfu does not use the cleaned text. An input name, a host or a path is
// compared, written or handed to nix as it was given, so that it still names
// the same thing; the warning shows how it reads once cleaned and leaves the
// decision to the user.
func ArgWarning(what, raw string) termsafe.Text {
	shown, rep := termsafe.Mark.LineReport(raw)
	if !rep.Changed() {
		return termsafe.Text{}
	}
	return Words(fmt.Sprintf(
		"WARNING: %s holds %s. It is shown as %q and used as it was given. "+
			"Text like that can make one name pass for another: check it before you go on.",
		what, rep, shown.String()))
}

// Typed cleans text that goes into one of the TUIs' fields: typed, pasted, or
// put there from a stored value that the user then edits. The field holds the
// cleaned text and nfu goes on to use it, so when cleaning changed something
// the warning says so; otherwise it is empty. what starts the sentence: "The
// pasted text".
//
// Bidirectional controls and invisible characters are removed here, not
// marked: a marker in a host name or an argument would become part of it.
func Typed(what, raw string) (cleaned string, warning termsafe.Text) {
	text, rep := termsafe.Drop.LineReport(raw)
	if !rep.Changed() {
		return text.String(), termsafe.Text{}
	}
	return text.String(), Words(fmt.Sprintf(
		"WARNING: %s held %s. nfu removed that and uses the cleaned text. "+
			"Input like that may be malicious: check it before you go on.", what, rep))
}

// InputNameNote says why a flake input's name deserves a second look, and is
// empty for an ordinary name.
//
// The rule for an input name is nix's own: `nix flake update <name>` takes
// only names of ASCII letters, digits, "_" and "-" that start with a letter.
// A flake.lock can hold any string as a name, so a name outside that rule is
// either one nix will refuse to update or one made to look like another, and
// both are worth a note. This is stricter than looking for mixed scripts and
// catches what that misses, such as a name written wholly in Cyrillic.
func InputNameNote(name string) termsafe.Text {
	if isFlakeID(name) {
		return termsafe.Text{}
	}
	return noteOf(append(oddities(name), "nix updates only names of ASCII letters, digits, _ and -"))
}

// HostNameNote says why a deploy host's name deserves a second look, and is
// empty for an ordinary name.
//
// A host is an attribute of the flake's deploy.nodes and may be any string,
// so there is no registry rule to hold it to. nfu notes a name that cleaning
// changed, a word in it that mixes scripts (a Latin name with one Cyrillic
// letter), and otherwise any name that is not plain ASCII: among ASCII names
// that is what a look-alike is, and the mixed-script check cannot see a name
// written wholly in another script.
func HostNameNote(name string) termsafe.Text {
	odd := oddities(name)
	if len(odd) == 0 && !isASCII(name) {
		odd = append(odd, "not plain ASCII")
	}
	return noteOf(odd)
}

// oddities lists what cleaning took out of name or marked in it, and its
// words that mix scripts.
func oddities(name string) []string {
	var parts []string
	text, rep := termsafe.Mark.LineReport(name)
	if rep.Changed() {
		parts = append(parts, "holds "+rep.String())
	}
	for _, m := range text.MixedScripts() {
		parts = append(parts, m.String())
	}
	return parts
}

func noteOf(parts []string) termsafe.Text {
	if len(parts) == 0 {
		return termsafe.Text{}
	}
	return Words(strings.Join(parts, "; "))
}

// isFlakeID reports whether s matches [a-zA-Z][a-zA-Z0-9_-]*, the form nix
// requires of an input name on its command line (flakeIdRegexS in nix's
// libflake).
func isFlakeID(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z':
		case i > 0 && (c >= '0' && c <= '9' || c == '_' || c == '-'):
		default:
			return false
		}
	}
	return s != ""
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}
