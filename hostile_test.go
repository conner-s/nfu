package main

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"

	"forgejo.kube.internal.conn3r.xyz/connerlab/termsafe"

	"github.com/conner-s/nfu/internal/config"
	"github.com/conner-s/nfu/internal/flake"
	"github.com/conner-s/nfu/internal/project"
	"github.com/conner-s/nfu/internal/ui"
)

// The plain (non-TUI) commands print text nfu did not write: flake.lock
// fields, project files, paths, and errors that quote nix, git and the command
// line. These tests put every sample of the termsafe kit into each of those
// positions and check what would be printed. internal/tui/hostile_test.go
// does the same for the TUIs, and docs/untrusted-text.md is the table both
// follow.

// printed are the ways outside text reaches the plain output. Each returns
// what nfu would write to the terminal with text in that position.
var printed = []struct {
	name string
	out  func(text string) string
	// quoted marks output that holds text Go's %q has escaped: see allowed.
	quoted bool
}{
	{name: "title line", out: func(text string) string {
		return titleLine(ui.NewStyles(), "/home/user/"+text)
	}},
	{name: "update footer", out: func(text string) string {
		var out, errOut bytes.Buffer
		results := []flake.UpdateResult{{Input: text, OldPin: "old", NewPin: "new"}}
		emitFooter(&out, &errOut, ui.NewStyles(), results, true,
			ui.Line("flake.lock: update "+text), ui.Line("commit failed: git: fatal: "+text), nil)
		return out.String() + errOut.String()
	}},
	{name: "error from nix", out: func(text string) string {
		return errorOutput(fmt.Errorf("updating nixpkgs: %w", &flake.CmdError{
			Name: "nix", Err: errors.New("exit status 1"),
			Stderr: []byte("error: unable to download " + text + "\n       second line " + text),
		}))
	}},
	{name: "error naming an unknown input", quoted: true, out: func(text string) string {
		return errorOutput(&flake.UnknownInputError{Name: text, Available: []string{"nixpkgs", "also" + text}})
	}},
	{name: "error from the flag parser", out: func(text string) string {
		return errorOutput(fmt.Errorf("flag provided but not defined: -%s", text))
	}},
	{name: "error for an unknown project", quoted: true, out: func(text string) string {
		_, err := project.BuildPlan(project.BuildCatalog(&config.Config{}, &project.Index{}, nil), text, true)
		return errorOutput(err)
	}},
	{name: "argument warning", out: func(text string) string {
		var b bytes.Buffer
		warnArgs(&b, globalFlags{flake: "/home/user/" + text, inputs: text + ",b", hostArgs: stringList{"Host:" + text}}, []string{text})
		return b.String()
	}},
	{name: "plan", out: func(text string) string {
		return project.RenderPlan(ui.NewStyles(), hostilePlan(text).Text("/home/user/"+text))
	}},
	// One project of this catalog will not resolve, and the reason quotes
	// the name it could not find.
	{name: "project list", quoted: true, out: func(text string) string {
		cat := hostileCatalog(text)
		return renderProjectList(ui.NewStyles(), cat, cat.List(), "lab"+text)
	}},
	{name: "run summary", out: func(text string) string {
		return runSummary(ui.NewStyles(), text, project.Result{Switched: true, Deployed: []string{text, "second" + text}})
	}},
}

func hostilePlan(text string) project.Plan {
	return project.Plan{
		Name:        text,
		Inputs:      []string{text, "second" + text},
		Commit:      true,
		LocalSwitch: true,
		Hosts:       []project.HostStep{{Name: text, Args: []string{"--flag", text}}},
		Members:     []string{text, "member" + text},
	}
}

func hostileCatalog(text string) *project.Catalog {
	return project.BuildCatalog(&config.Config{
		Hosts: map[string]config.Host{"host" + text: {DeployArgs: []string{text}}},
		Projects: map[string]config.Project{
			"lab" + text:    {Inputs: []string{text}, Hosts: []string{"host" + text}},
			"broken" + text: {Includes: []string{"missing" + text}},
		},
	}, &project.Index{}, nil)
}

func errorOutput(err error) string {
	var b bytes.Buffer
	printError(&b, err)
	return b.String()
}

// An error that quotes a name with %q draws an ESC as the four characters
// \x1b, so the payload of a control string stands after it as text: what the
// name holds, readable and inert. The kit calls a drawn payload a problem,
// because from a cleaner it means half a sequence was removed. Where the
// escape is there to be read it is the intended picture; every other finding
// still fails.
func allowed(problem, out string, quoted bool) bool {
	if problem == "" {
		return true
	}
	if !quoted || !strings.Contains(problem, "the payload of a control sequence") {
		return false
	}
	return strings.Contains(out, `\x1b`) || strings.Contains(out, `\u0090`) || strings.Contains(out, `\u009d`)
}

func TestPlainOutput_HostileText(t *testing.T) {
	for _, p := range printed {
		for _, h := range termsafe.Samples() {
			t.Run(p.name+"/"+h.Name, func(t *testing.T) {
				out := p.out(h.Text)
				if problem := termsafe.ViewProblem(out); !allowed(problem, out, p.quoted) {
					t.Errorf("%s:\n%q", problem, out)
				}
				// Plain output goes to the terminal as it is, so the check
				// for what a terminal would act on applies to it too.
				if problem := termsafe.TerminalProblem(out); !allowed(problem, out, p.quoted) {
					t.Errorf("%s:\n%q", problem, out)
				}
			})
		}
	}
}

// An error whose text was cut by cleaning says so on a second line.
func TestPrintErrorSaysWhenALineWasCut(t *testing.T) {
	got := errorOutput(errors.New("nix failed: error: \x1bPnever terminated, and the reason was here"))
	want := "nfu: nix failed: error: \n" +
		"nfu: info: an unterminated control string was cut from the text above, with the rest of its line\n"
	if got != want {
		t.Errorf("printError wrote %q\n             want %q", got, want)
	}
	if got := errorOutput(errors.New("plain\nsecond line")); got != "nfu: plain\nsecond line\n" {
		t.Errorf("printError of a plain error wrote %q", got)
	}
}

// Ordinary arguments are not warned about; one that is not what it looks like
// is, and the run goes on with it as it was given.
func TestWarnArgs(t *testing.T) {
	var b bytes.Buffer
	warnArgs(&b, globalFlags{flake: "~/flake", inputs: "a,b", hosts: "Fabricator"}, []string{"nixpkgs", "@workstation", "日本語"})
	if b.Len() != 0 {
		t.Errorf("ordinary arguments drew a warning: %q", b.String())
	}
	rlo := string(rune(0x202E)) // right-to-left override
	warnArgs(&b, globalFlags{hosts: "Fab" + rlo + "ricator"}, []string{"nix" + rlo + "pkgs"})
	lines := strings.Split(strings.TrimRight(b.String(), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("want one warning per argument, got %q", lines)
	}
	if !strings.HasPrefix(lines[0], `nfu: WARNING: An argument holds 1 bidirectional control. It is shown as "nix<U+202E>pkgs"`) {
		t.Errorf("first warning = %q", lines[0])
	}
	if !strings.HasPrefix(lines[1], `nfu: WARNING: The value of --hosts holds 1 bidirectional control. It is shown as "Fab<U+202E>ricator"`) {
		t.Errorf("second warning = %q", lines[1])
	}
}

// The plan a person confirms names the hosts that are not what they look
// like. (An input that is not what it looks like never gets a plan: see
// TestBuildPlanRefusesInputNamesNixCannotUpdate in internal/project.)
func TestRenderPlanNotesOddNames(t *testing.T) {
	cyrA := string(rune(0x0430)) // Cyrillic a
	plain := project.Plan{Name: "lab", Inputs: []string{"nixpkgs"}, Hosts: []project.HostStep{{Name: "Fabricator"}}}
	if out := project.RenderPlan(ui.NewStyles(), plain.Text("/flake")); strings.Contains(out, "!") {
		t.Errorf("a plan of ordinary names carries a note:\n%s", out)
	}
	odd := project.Plan{Name: "lab", Inputs: []string{"nixpkgs"}, Hosts: []project.HostStep{{Name: "F" + cyrA + "bricator"}}}
	out := project.RenderPlan(ui.NewStyles(), odd.Text("/flake"))
	for _, want := range []string{
		"! host F" + cyrA + "bricator: ", "mixes Cyrillic and Latin letters (U+0430 is Cyrillic)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the plan does not say %q:\n%s", want, out)
		}
	}
}
