package main

import (
	"flag"
	"io"
	"reflect"
	"testing"
)

func parseArgs(t *testing.T, args []string) ([]string, globalFlags) {
	t.Helper()
	fs := flag.NewFlagSet("nfu", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var g globalFlags
	addFlags(fs, &g)
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		t.Fatalf("parse %v: %v", args, err)
	}
	return pos, g
}

func TestParseInterspersed(t *testing.T) {
	// Flags after positionals are honored (`nfu totality --commit`).
	pos, g := parseArgs(t, []string{"totality", "--commit"})
	if !reflect.DeepEqual(pos, []string{"totality"}) || !g.commit {
		t.Errorf("totality --commit: pos=%v commit=%v", pos, g.commit)
	}

	// Flags before positionals too.
	pos, g = parseArgs(t, []string{"-c", "totality", "npk"})
	if !reflect.DeepEqual(pos, []string{"totality", "npk"}) || !g.commit {
		t.Errorf("-c totality npk: pos=%v commit=%v", pos, g.commit)
	}
}

func TestDoubleDashProtectsAllPositionals(t *testing.T) {
	// Everything after `--` is literal — including dash-prefixed tokens, and
	// there can be more than one (the old single-positional escape was a bug).
	pos, g := parseArgs(t, []string{"--", "foo", "--commit"})
	if !reflect.DeepEqual(pos, []string{"foo", "--commit"}) {
		t.Errorf("-- foo --commit: pos=%v", pos)
	}
	if g.commit {
		t.Errorf("--commit after -- must NOT be parsed as a flag")
	}

	// Flags before `--` still apply; positionals after are literal.
	pos, g = parseArgs(t, []string{"--commit", "--", "a", "b"})
	if !reflect.DeepEqual(pos, []string{"a", "b"}) || !g.commit {
		t.Errorf("--commit -- a b: pos=%v commit=%v", pos, g.commit)
	}
}
