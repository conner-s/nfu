package main

import (
	"bytes"
	"go/scanner"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/conner-s/termsafe"

	"github.com/conner-s/nfu/internal/ui"
)

// The plain output on a terminal that is not known to decode UTF-8: the same
// positions as TestPlainOutput_HostileText, each written the way the command
// writes it, through ui.Fprint. Not one byte outside ASCII may come out, in
// the outside text or in nfu's own bullets and arrows.
func TestPlainOutput_HostileText_NotUTF8(t *testing.T) {
	defer ui.SetTerminal(notUTF8Terminal)()
	for _, p := range printed {
		for _, h := range termsafe.Samples() {
			t.Run(p.name+"/"+h.Name, func(t *testing.T) {
				var b bytes.Buffer
				ui.Fprint(&b, p.out(h.Text))
				out := b.String()
				if problem := termsafe.ViewProblem(out); !allowed(problem, out, p.quoted) {
					t.Errorf("%s:\n%q", problem, out)
				}
				if problem := termsafe.TerminalProblem(out); !allowed(problem, out, p.quoted) {
					t.Errorf("%s:\n%q", problem, out)
				}
				if problem := termsafe.ASCIIProblem(out); problem != "" {
					t.Errorf("%s:\n%q", problem, out)
				}
			})
		}
	}
}

// sources calls visit for every Go file of the program that is not a test.
// A symlinked directory is not followed.
func sources(t *testing.T, visit func(path string, src []byte)) {
	t.Helper()
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		// vendor is other people's code: the Nix build puts the modules there.
		if d.IsDir() && (d.Name() == "vendor" || (strings.HasPrefix(d.Name(), ".") && path != ".")) {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		visit(path, src)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// Everything nfu prints for a person goes through ui.Fprint, ui.Fprintf or
// ui.Fprintln, which is where its own glyphs give way to ASCII on a terminal
// that is not UTF-8. A print that went around them would not. The two that
// may are data for another program: --json and --print.
func TestEveryPrintGoesThroughTheTerminalFit(t *testing.T) {
	data := []string{
		`fmt.Printf("# projects/%s.toml\n%s", name, data)`, // --print
		`enc := json.NewEncoder(os.Stdout)`,                // --json
	}
	sources(t, func(path string, src []byte) {
		if strings.HasPrefix(path, filepath.Join("internal", "ui")) {
			return // where ui.Fprint itself is
		}
		for i, line := range strings.Split(string(src), "\n") {
			code, _, _ := strings.Cut(line, "//")
			direct := strings.Contains(code, "fmt.Print") ||
				((strings.Contains(code, "fmt.Fprint") || strings.Contains(code, "NewEncoder(")) && strings.Contains(code, "os.Std"))
			if !direct {
				continue
			}
			known := false
			for _, d := range data {
				known = known || strings.Contains(code, d)
			}
			if !known {
				t.Errorf("%s:%d writes to the terminal past ui.Fprint: %s", path, i+1, strings.TrimSpace(line))
			}
		}
	})
}

// Every character outside ASCII that nfu's own source draws has an ASCII
// stand-in one column wide (ui.Own). One added without a stand-in would be
// written by its code point, eight columns where one was laid out.
func TestEveryGlyphHasAnASCIIStandIn(t *testing.T) {
	defer ui.SetTerminal(notUTF8Terminal)()
	seen := map[rune]string{}
	sources(t, func(path string, src []byte) {
		var s scanner.Scanner
		fset := token.NewFileSet()
		s.Init(fset.AddFile(path, fset.Base(), len(src)), src, nil, 0)
		for {
			_, tok, lit := s.Scan()
			if tok == token.EOF {
				break
			}
			if tok != token.STRING && tok != token.CHAR {
				continue
			}
			text := lit
			if tok == token.STRING {
				if u, err := strconv.Unquote(lit); err == nil {
					text = u
				}
			}
			for _, r := range text {
				if r >= utf8.RuneSelf {
					seen[r] = path
				}
			}
		}
	})
	if len(seen) < 20 {
		t.Fatalf("found only %d characters outside ASCII in the source; the scan is not reading it", len(seen))
	}
	for r, path := range seen {
		if stand := ui.Own(string(r)); len(stand) != 1 {
			t.Errorf("U+%04X %q (in %s) has no stand-in: on a terminal that is not UTF-8 it is written as %q", r, string(r), path, stand)
		}
	}
}

// useTerminal settles the verdict from --charset and says so once when the
// terminal is not known to decode UTF-8.
func TestUseTerminal(t *testing.T) {
	defer ui.SetTerminal(utf8Terminal)()
	noConfig := filepath.Join(t.TempDir(), "none.toml")

	var w bytes.Buffer
	if err := useTerminal(&w, globalFlags{config: noConfig, charset: "utf8"}); err != nil || !ui.UTF8() || w.Len() != 0 {
		t.Errorf("--charset=utf8: err %v, UTF8 %v, wrote %q; want UTF-8 and no warning", err, ui.UTF8(), w.String())
	}

	w.Reset()
	if err := useTerminal(&w, globalFlags{config: noConfig, charset: "ascii"}); err != nil || ui.UTF8() {
		t.Errorf("--charset=ascii: err %v, UTF8 %v", err, ui.UTF8())
	}
	got := w.String()
	if !strings.HasPrefix(got, "nfu: WARNING: this terminal is not known to decode UTF-8 (charset ascii was chosen). ") ||
		!strings.Contains(got, "--charset=utf8") || strings.Count(got, "\n") != 1 {
		t.Errorf("--charset=ascii wrote %q, want the warning, once", got)
	}
	if p := termsafe.ASCIIProblem(got); p != "" {
		t.Error(p)
	}

	// --json draws nothing, so it is not told.
	w.Reset()
	if err := useTerminal(&w, globalFlags{config: noConfig, charset: "ascii", asJSON: true}); err != nil || w.Len() != 0 {
		t.Errorf("--json --charset=ascii: err %v, wrote %q; want nothing", err, w.String())
	}

	if err := useTerminal(&w, globalFlags{config: noConfig, charset: "latin1"}); err == nil ||
		err.Error() != `--charset must be auto, utf8 or ascii, not "latin1"` {
		t.Errorf("--charset=latin1: %v", err)
	}
}

// The config's `charset` is the lasting form of the flag, and the flag wins.
func TestUseTerminalFromConfig(t *testing.T) {
	defer ui.SetTerminal(utf8Terminal)()
	cfg := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(cfg, []byte("charset = \"ascii\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var w bytes.Buffer
	if err := useTerminal(&w, globalFlags{config: cfg}); err != nil || ui.UTF8() || w.Len() == 0 {
		t.Errorf("charset = ascii in the config: err %v, UTF8 %v, wrote %q", err, ui.UTF8(), w.String())
	}
	w.Reset()
	if err := useTerminal(&w, globalFlags{config: cfg, charset: "utf8"}); err != nil || !ui.UTF8() || w.Len() != 0 {
		t.Errorf("--charset=utf8 over the config: err %v, UTF8 %v, wrote %q", err, ui.UTF8(), w.String())
	}
	if err := os.WriteFile(cfg, []byte("charset = \"latin1\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadConfig(cfg); err == nil || !strings.Contains(err.Error(), "charset must be auto, utf8 or ascii") {
		t.Errorf("charset = latin1 in the config: %v, want it refused at load", err)
	}
}
