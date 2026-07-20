package proc

import (
	"context"
	"errors"
	"testing"
)

func TestRunLinesCapturesBothStreams(t *testing.T) {
	var got []string
	err := RunLines(context.Background(), t.TempDir(), func(l string) { got = append(got, l) },
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
	err := RunLines(context.Background(), t.TempDir(), func(l string) { got = append(got, l) },
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
	err := RunLines(context.Background(), t.TempDir(), func(l string) { got = append(got, l) },
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
	err := RunLines(context.Background(), t.TempDir(), func(l string) { got = append(got, l) },
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

func TestStripControl(t *testing.T) {
	cases := map[string]string{
		"\x1b[1;32m✓ ok\x1b[0m":    "✓ ok",
		"\x1b]8;;http://x\x07link": "link",
		"plain":                    "plain",
		"tab\tkept":                "tab\tkept",
		"bell\x07 gone":            "bell gone",
		"\x1b[2K\x1b[1Goverwrite":  "overwrite",
	}
	for in, want := range cases {
		if got := stripControl(in); got != want {
			t.Errorf("stripControl(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRunLinesMissingBinary(t *testing.T) {
	err := RunLines(context.Background(), t.TempDir(), func(string) {}, "definitely-not-a-real-binary-xyz")
	if err == nil {
		t.Fatal("expected an error for a missing binary")
	}
}
