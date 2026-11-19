package flake

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestIsInputName(t *testing.T) {
	for _, ok := range []string{"nixpkgs", "home-manager", "nixpkgs_2", "A", "a-", "flake-utils"} {
		if !IsInputName(ok) {
			t.Errorf("IsInputName(%q) = false, want true", ok)
		}
	}
	cyrA := string(rune(0x0430)) // Cyrillic a
	for _, bad := range []string{
		"", "-n", "--refresh", "--override-input", "-", "2fast", "_x", "we ird", "a/b", "a.b", "a=b",
		"nixp" + cyrA + "kgs", "nixpkgs\n", "nixpkgs\x00",
	} {
		if IsInputName(bad) {
			t.Errorf("IsInputName(%q) = true, want false", bad)
		}
	}
}

func TestCheckInputNames(t *testing.T) {
	if err := CheckInputNames("nixpkgs", "home-manager"); err != nil {
		t.Errorf("ordinary names refused: %v", err)
	}
	err := CheckInputNames("nixpkgs", "--refresh")
	var inv *InvalidInputError
	if !errors.As(err, &inv) || !reflect.DeepEqual(inv.Names, []string{"--refresh"}) {
		t.Fatalf("CheckInputNames = %v, want an InvalidInputError for --refresh", err)
	}
	want := `"--refresh" is not a name nix can update: an input's name is ASCII letters, digits, "_" and "-", and starts with a letter`
	if err.Error() != want {
		t.Errorf("error = %q\n want %q", err, want)
	}
	if got := CheckInputNames("-n", "we ird").Error(); !strings.HasPrefix(got, `"-n", "we ird" are not names nix can update`) {
		t.Errorf("error for two names = %q", got)
	}
}

// The rule comes before the lock: a lock that holds an input named like an
// option must not be what lets the name through.
func TestValidateNamesRefusesAFlagNamedInputOfTheLock(t *testing.T) {
	all := flagNamedInputs(t)
	byName := indexByName(all)
	if _, ok := byName["--refresh"]; !ok {
		t.Fatal("the fixture lock should hold an input named --refresh")
	}
	for _, name := range []string{"--refresh", "-n", "we ird"} {
		err := validateNames([]string{"dep", name}, byName, all)
		var inv *InvalidInputError
		if !errors.As(err, &inv) || !reflect.DeepEqual(inv.Names, []string{name}) {
			t.Errorf("validateNames(dep, %q) = %v, want an InvalidInputError for it", name, err)
		}
	}
	if err := validateNames([]string{"dep"}, byName, all); err != nil {
		t.Errorf("the ordinary input of the same lock is refused: %v", err)
	}
}

func TestUpdateArgsEndOptionsBeforeNames(t *testing.T) {
	c := New("/flake")
	got := c.updateArgs([]string{"nixpkgs", "nfu"})
	want := []string{"--extra-experimental-features", "nix-command flakes", "flake", "update", "--", "nixpkgs", "nfu"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("updateArgs = %q\n      want %q", got, want)
	}
	// With no names there is nothing to guard, and nix updates every input.
	if got := c.updateArgs(nil); got[len(got)-1] != "update" {
		t.Errorf("updateArgs(nil) = %q, want it to end at `update`", got)
	}
	// Nothing after `--` is an option, so the probe's output file comes first.
	got = upstreamArgs("nixpkgs", "/tmp/x/flake.lock", true)
	want = []string{"--extra-experimental-features", "nix-command flakes", "--refresh",
		"flake", "update", "--output-lock-file", "/tmp/x/flake.lock", "--", "nixpkgs"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("upstreamArgs = %q\n        want %q", got, want)
	}
}

// flagNamedInputs reads testdata/flag-named/flake.lock: the lock nix 2.35
// itself wrote for a flake.nix with inputs.dep, inputs."--refresh",
// inputs."-n" and inputs."we ird".
func flagNamedInputs(t *testing.T) []Input {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "flag-named", "flake.lock"))
	if err != nil {
		t.Fatal(err)
	}
	md, err := parseLockFile(data)
	if err != nil {
		t.Fatal(err)
	}
	inputs, err := md.buildInputs()
	if err != nil {
		t.Fatal(err)
	}
	return inputs
}

// stubNix puts a stand-in for nix first on PATH and returns a flake root that
// holds the flag-named lock, and a function that returns the argument lists
// the stand-in was called with. It answers `flake metadata` with the root's
// lock, writes that lock to an --output-lock-file, and otherwise does nothing:
// what is under test is which commands nfu runs, and with which arguments.
func stubNix(t *testing.T) (root string, calls func() [][]string) {
	t.Helper()
	root = t.TempDir()
	lock, err := os.ReadFile(filepath.Join("testdata", "flag-named", "flake.lock"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "flake.lock"), lock, 0o600); err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	log := filepath.Join(bin, "calls")
	script := `#!/bin/sh
{ for a in "$@"; do printf '%s\037' "$a"; done; printf '\n'; } >> "` + log + `"
out=
prev=
for a in "$@"; do
  if [ "$prev" = --output-lock-file ]; then out=$a; fi
  if [ "$a" = metadata ]; then printf '{"locks":'; cat flake.lock; printf '}'; exit 0; fi
  prev=$a
done
if [ -n "$out" ]; then cp flake.lock "$out"; fi
exit 0
`
	if err := os.WriteFile(filepath.Join(bin, "nix"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	calls = func() [][]string {
		data, err := os.ReadFile(log)
		if err != nil {
			return nil
		}
		var out [][]string
		for _, line := range strings.Split(strings.TrimRight(string(data), "\n"), "\n") {
			out = append(out, strings.Split(strings.TrimSuffix(line, "\x1f"), "\x1f"))
		}
		return out
	}
	return root, calls
}

// updates returns the calls that were `nix flake update`, each from the word
// `update` on.
func updates(calls [][]string) [][]string {
	var out [][]string
	for _, c := range calls {
		for i, a := range c {
			if a == "update" && i > 0 && c[i-1] == "flake" {
				out = append(out, c[i:])
			}
		}
	}
	return out
}

type quietReporter struct{}

func (quietReporter) Plan([]string)     {}
func (quietReporter) Start(string)      {}
func (quietReporter) Done(UpdateResult) {}

// End to end against a stand-in nix: for a lock with an input named like an
// option, nfu runs no `nix flake update` at all when that input is asked for,
// and puts an ordinary name after `--`.
func TestNoNixCommandForAFlagNamedInput(t *testing.T) {
	ctx := context.Background()

	for _, name := range []string{"--refresh", "-n", "we ird"} {
		root, calls := stubNix(t)
		c := New(root)
		var inv *InvalidInputError
		if _, err := c.Update(ctx, "dep", name); !errors.As(err, &inv) {
			t.Errorf("Update(dep, %q) = %v, want an InvalidInputError", name, err)
		}
		if _, err := c.UpdateSeq(ctx, quietReporter{}, name); !errors.As(err, &inv) {
			t.Errorf("UpdateSeq(%q) = %v, want an InvalidInputError", name, err)
		}
		if _, err := c.Upstream(ctx, name, false); !errors.As(err, &inv) {
			t.Errorf("Upstream(%q) = %v, want an InvalidInputError", name, err)
		}
		if got := updates(calls()); len(got) != 0 {
			t.Errorf("asking for %q ran nix flake update: %q", name, got)
		}
	}

	root, calls := stubNix(t)
	c := New(root)
	if _, err := c.Update(ctx, "dep"); err != nil {
		t.Fatalf("Update(dep): %v", err)
	}
	if _, err := c.Upstream(ctx, "dep", false); err != nil {
		t.Fatalf("Upstream(dep): %v", err)
	}
	got := updates(calls())
	if len(got) != 2 {
		t.Fatalf("want two nix flake update calls, got %q", got)
	}
	if !reflect.DeepEqual(got[0], []string{"update", "--", "dep"}) {
		t.Errorf("update ran as %q, want update -- dep", got[0])
	}
	if n := len(got[1]); n != 5 || got[1][1] != "--output-lock-file" || got[1][3] != "--" || got[1][4] != "dep" {
		t.Errorf("probe ran as %q, want update --output-lock-file <file> -- dep", got[1])
	}
}

// "Every input" is every input nix can be asked for by name. The others are
// left as they were, and the run says so.
func TestUpdateSeqOfEveryInputLeavesOutWhatNixCannotName(t *testing.T) {
	root, calls := stubNix(t)
	results, err := New(root).UpdateSeq(context.Background(), quietReporter{})
	if len(results) != 1 || results[0].Input != "dep" {
		t.Errorf("results = %+v, want dep alone", results)
	}
	var inv *InvalidInputError
	if !errors.As(err, &inv) || !reflect.DeepEqual(inv.Names, []string{"--refresh", "-n", "we ird"}) {
		t.Fatalf("err = %v, want an InvalidInputError for the three other inputs", err)
	}
	if !strings.HasPrefix(err.Error(), `not updated: "--refresh", "-n", "we ird" are not names nix can update`) {
		t.Errorf("err = %q", err)
	}
	if got := updates(calls()); len(got) != 1 || !reflect.DeepEqual(got[0], []string{"update", "--", "dep"}) {
		t.Errorf("nix flake update calls = %q, want one, for dep", got)
	}
}
