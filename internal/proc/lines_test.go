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

func TestRunLinesMissingBinary(t *testing.T) {
	err := RunLines(context.Background(), t.TempDir(), func(string) {}, "definitely-not-a-real-binary-xyz")
	if err == nil {
		t.Fatal("expected an error for a missing binary")
	}
}
