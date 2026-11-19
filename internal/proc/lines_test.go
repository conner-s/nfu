package proc

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/conner-s/termsafe"
)

// collect returns an onLine callback that appends each cleaned line to got.
func collect(got *[]string) func(termsafe.Text, termsafe.Report) {
	return func(l termsafe.Text, _ termsafe.Report) { *got = append(*got, l.String()) }
}

func TestRunLinesCapturesBothStreams(t *testing.T) {
	var got []string
	err := RunLines(context.Background(), t.TempDir(), collect(&got),
		"sh", "-c", "echo out; echo err 1>&2; echo tail")
	if err != nil {
		t.Fatalf("RunLines: %v", err)
	}
	if len(got) != 3 || got[0] != "out" || got[1] != "err" || got[2] != "tail" {
		t.Fatalf("unexpected lines: %q", got)
	}
}

func TestRunLinesReportsExit(t *testing.T) {
	var got []string
	err := RunLines(context.Background(), t.TempDir(), collect(&got),
		"sh", "-c", "echo boom; exit 3")
	var ce *CmdError
	if !errors.As(err, &ce) {
		t.Fatalf("expected CmdError, got %v", err)
	}
	if len(got) != 1 || got[0] != "boom" {
		t.Fatalf("output before failure should still stream: %q", got)
	}
}

func TestRunLinesSanitizesTerminalOutput(t *testing.T) {
	var got []string
	// A nom-style progress redraw: color codes, cursor movement, \r overwrites,
	// and a \r\n pair. Everything should land as clean, separate lines.
	err := RunLines(context.Background(), t.TempDir(), collect(&got),
		"sh", "-c", `printf '\033[32mbuilding\033[0m a\r\033[1Abuilding b\rdone\r\nnext\n'`)
	if err != nil {
		t.Fatalf("RunLines: %v", err)
	}
	want := []string{"building a", "building b", "done", "next"}
	if len(got) != len(want) {
		t.Fatalf("unexpected lines: %q", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("line %d: got %q, want %q", i, got[i], want[i])
		}
	}
}

func TestRunLinesCollapsesBlankRuns(t *testing.T) {
	var got []string
	err := RunLines(context.Background(), t.TempDir(), collect(&got),
		"sh", "-c", `printf '\n\na\n\n\n\nb\n\n'`)
	if err != nil {
		t.Fatalf("RunLines: %v", err)
	}
	want := []string{"a", "", "b", ""}
	if len(got) != len(want) {
		t.Fatalf("unexpected lines: %q", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("line %d: got %q, want %q", i, got[i], want[i])
		}
	}
}

// What a line of nh or deploy output looks like once RunLines has cleaned it.
// This table replaced the one for stripControl, the regular expression this
// package used before termsafe. Two rows changed meaning: a tab is now a
// space, and the payload of a DCS, APC, PM or SOS string goes with the string
// instead of staying behind as text.
func TestRunLinesCleansEachLine(t *testing.T) {
	const (
		rlo  = "\u202e" // right-to-left override
		zwsp = "\u200b" // zero width space
		c1   = "\u009b" // CSI as one C1 character
	)
	cases := []struct{ in, want string }{
		{"\x1b[1;32m✓ ok\x1b[0m", "✓ ok"},
		{"\x1b]8;;http://x\x07link", "link"},
		{"plain", "plain"},
		{"tab\tnow a space", "tab now a space"},
		{"bell\x07 gone", "bell gone"},
		{"\x1b[2K\x1b[1Goverwrite", "overwrite"},
		{"dcs\x1bP+q544e\x1b\\tail", "dcstail"},
		{"apc\x1b_Gf=100;544e\x1b\\tail", "apctail"},
		{"c1 " + c1 + "2Jtail", "c1 tail"},
		{"bidi" + rlo + "tail", "biditail"},
		{"zero" + zwsp + "width", "zerowidth"},
	}
	for _, c := range cases {
		var got []string
		err := RunLines(context.Background(), t.TempDir(), collect(&got), "printf", "%s\n", c.in)
		if err != nil {
			t.Fatalf("RunLines(%q): %v", c.in, err)
		}
		if len(got) != 1 || got[0] != c.want {
			t.Errorf("a line %q came out as %q, want %q", c.in, got, c.want)
		}
	}
}

// A control string with no terminator takes the rest of its line with it. The
// report says so, so the run pane can; and such a line is delivered even when
// nothing of it is left, or the note would have nothing to stand under.
func TestRunLinesReportsACutLine(t *testing.T) {
	var lines []string
	var cut []int
	err := RunLines(context.Background(), t.TempDir(), func(l termsafe.Text, rep termsafe.Report) {
		lines = append(lines, l.String())
		cut = append(cut, rep.Unterminated)
	}, "printf", "%s\n", "before\x1bPnever ended", "\x1b_all of it", "after")
	if err != nil {
		t.Fatalf("RunLines: %v", err)
	}
	wantLines, wantCut := []string{"before", "", "after"}, []int{1, 1, 0}
	if len(lines) != len(wantLines) {
		t.Fatalf("lines = %q, want %q", lines, wantLines)
	}
	for i := range wantLines {
		if lines[i] != wantLines[i] || cut[i] != wantCut[i] {
			t.Errorf("line %d = %q with %d cut, want %q with %d", i, lines[i], cut[i], wantLines[i], wantCut[i])
		}
	}
}

// Every sample of the termsafe kit, printed by a child the way nh would print
// it, arrives with nothing left that cleaning would still remove. The child
// is cat on a file: one sample holds a NUL, which no argument can carry.
func TestRunLinesHostileSamples(t *testing.T) {
	for _, h := range termsafe.Samples() {
		dir := t.TempDir()
		file := filepath.Join(dir, "out")
		if err := os.WriteFile(file, []byte(h.Text+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		var got []string
		if err := RunLines(context.Background(), dir, collect(&got), "cat", file); err != nil {
			t.Fatalf("%s: RunLines: %v", h.Name, err)
		}
		if len(got) != 1 {
			t.Errorf("%s: %d lines %q, want one", h.Name, len(got), got)
			continue
		}
		if p := termsafe.ViewProblem(got[0]); p != "" {
			t.Errorf("%s: %s: %q", h.Name, p, got[0])
		}
	}
}

func TestRunLinesMissingBinary(t *testing.T) {
	err := RunLines(context.Background(), t.TempDir(), func(termsafe.Text, termsafe.Report) {}, "definitely-not-a-real-binary-xyz")
	if err == nil {
		t.Fatal("expected an error for a missing binary")
	}
}
