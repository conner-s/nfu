package ui

import (
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"

	"charm.land/lipgloss/v2"
	"github.com/conner-s/termsafe"
)

// What a termsafe.Text promises holds on a terminal that decodes UTF-8. On
// any other, the bytes of ordinary characters are controls (U+00DB is C3 9B,
// and 9B is CSI), and only ASCII is safe. nfu therefore does not assume a
// UTF-8 terminal. This file holds the verdict and the two things that follow
// from it:
//
//   - Fit: outside text is drawn in its ASCII form, every other character
//     shown by its code point. Every conversion in text.go and notice.go ends
//     in it, so it comes before any truncation or padding.
//   - Own: what nfu has put together to write (a view, a line of output)
//     passes through it last. On a UTF-8 terminal it does nothing. On any
//     other it gives nfu's own glyphs, lipgloss's borders and the spinners
//     their ASCII stand-ins and writes whatever else is not ASCII by its code
//     point, so that not one byte outside ASCII is written whatever was
//     missed further up.
//
// The verdict is set once, in main, before anything is printed. Until it is
// set nothing is known about the terminal, and that means ASCII.

var terminal termsafe.Terminal

// SetTerminal makes t the verdict everything goes by, and returns a function
// that puts the previous one back (for tests).
func SetTerminal(t termsafe.Terminal) (restore func()) {
	old := terminal
	terminal = t
	return func() { terminal = old }
}

// UTF8 reports whether the terminal can be taken to decode UTF-8.
func UTF8() bool {
	return terminal.Encoding == termsafe.EncodingUTF8
}

// Fit returns t as the terminal can take it: t itself on a UTF-8 terminal,
// its ASCII form on any other.
func Fit(t termsafe.Text) termsafe.Text {
	return terminal.Fit(t)
}

// Charsets are the values of --charset and of the `charset` config key.
const Charsets = "auto, utf8 or ascii"

// ChooseTerminal turns a --charset value into a verdict. "auto", and no
// value, ask termsafe.CheckTerminal about out: on Unix that reads the locale.
// "utf8" and "ascii" overrule it, one in each direction: the check cannot see
// how the terminal emulator is set, and many terminals under LANG=C do decode
// UTF-8.
func ChooseTerminal(charset string, out *os.File) (termsafe.Terminal, error) {
	switch strings.ToLower(charset) {
	case "", "auto":
		return termsafe.CheckTerminal(out), nil
	case "utf8", "utf-8":
		return termsafe.Terminal{
			Encoding: termsafe.EncodingUTF8,
			Reason:   termsafe.Drop.Line("charset utf8 was chosen"),
		}, nil
	case "ascii":
		return termsafe.Terminal{
			Encoding: termsafe.EncodingNotUTF8,
			Reason:   termsafe.Drop.Line("charset ascii was chosen"),
		}, nil
	}
	return termsafe.Terminal{}, fmt.Errorf("charset must be %s, not %q", Charsets, charset)
}

// TerminalWarning says, once per run, that the terminal is not known to
// decode UTF-8, why, what nfu does about it and how to overrule it. It is
// empty on a UTF-8 terminal. It is ASCII: it has to be readable on the
// terminal it is about.
func TerminalWarning() termsafe.Text {
	if UTF8() {
		return termsafe.Text{}
	}
	why := ""
	if !terminal.Reason.IsEmpty() {
		why = " (" + terminal.Reason.String() + ")"
	}
	return Words("WARNING: this terminal is not known to decode UTF-8" + why + ". " +
		"nfu draws ASCII only and shows any other character by its code point, such as <U+00E9>. " +
		"If this terminal does decode UTF-8, pass --charset=utf8 or set charset = \"utf8\" in the config.")
}

// Own returns s, something nfu is about to write, as the terminal can take
// it. s is made of nfu's own words and glyphs, its styling, and outside text
// that Fit has already brought to ASCII.
func Own(s string) string {
	if UTF8() || isASCII(s) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		if s[i] < utf8.RuneSelf {
			b.WriteByte(s[i])
			i++
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		i += size
		switch stand, ok := standIns[r]; {
		case ok:
			b.WriteString(stand)
		case r == utf8.RuneError && size == 1:
			b.WriteByte('?')
		default:
			fmt.Fprintf(&b, "<U+%04X>", r)
		}
	}
	return b.String()
}

// standIns are the ASCII stand-ins for what nfu and its libraries draw. Each
// is one column wide, like the glyph it stands for, so no layout moves.
var standIns = func() map[rune]string {
	m := map[rune]string{
		'·': "-", '—': "-", '…': ".",
		'→': ">", '↑': "^", '↓': "v", '↳': ">",
		'✓': "+", '✗': "x", '•': "*", '●': "*", '○': "o",
		'▸': ">", '❯': ">", '‹': "<", '›': ">",
		'↻': "@", '❄': "*", '➤': ">",
		'█': "#", '░': ".", '▏': "|",
		// lipgloss.RoundedBorder
		'│': "|", '─': "-", '╭': "+", '╮': "+", '╰': "+", '╯': "+",
	}
	// The two spinners: the plain one's frames (spinnerFrames) and those of
	// bubbles' spinner.Dot. Each frame gets the next of | / - \, so they
	// still turn.
	const turn = `|/-\`
	for i, f := range spinnerFrames {
		for _, r := range f {
			m[r] = string(turn[i%len(turn)])
		}
	}
	for i, r := range []rune{0x28FE, 0x28FD, 0x28FB, 0x28BF, 0x287F, 0x28DF, 0x28EF, 0x28F7} {
		m[r] = string(turn[i%len(turn)])
	}
	return m
}()

// Fprint, Fprintf and Fprintln are fmt's, with what is written passed through
// Own. Everything nfu prints for a person goes through one of them. What it
// prints for a program (--json, --print) does not: that is data.
func Fprint(w io.Writer, a ...any) {
	fmt.Fprint(w, Own(fmt.Sprint(a...)))
}

func Fprintf(w io.Writer, format string, a ...any) {
	fmt.Fprint(w, Own(fmt.Sprintf(format, a...)))
}

func Fprintln(w io.Writer, a ...any) {
	fmt.Fprint(w, Own(fmt.Sprintln(a...)))
}

// TerminalBanner is TerminalWarning for the top of a TUI's view: its lines,
// wrapped to width when one is known (0 leaves the wrapping to the terminal).
// It is empty on a UTF-8 terminal. A TUI repeats the warning main printed,
// because the manager's alternate screen hides what was printed before it.
func TerminalBanner(width int) []string {
	w := TerminalWarning()
	if w.IsEmpty() {
		return nil
	}
	if width <= 0 {
		return []string{w.String()}
	}
	wrapped := lipgloss.NewStyle().Width(width).Render(w.String())
	lines := strings.Split(wrapped, "\n")
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], " ")
	}
	return lines
}
