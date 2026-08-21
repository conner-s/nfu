package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// setFixture writes a config with a projects directory holding a bundle over two
// leaf projects, and returns the flags that point nfu at it.
func setFixture(t *testing.T) globalFlags {
	t.Helper()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(cfgPath, []byte("flakeRoot = \"/nowhere\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	pdir := filepath.Join(dir, "projects")
	if err := os.MkdirAll(pdir, 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"npk.toml":         "inputs = ['npk', 'totality']\n",
		"nfu.toml":         "inputs = ['nfu']\n",
		"workstation.toml": "includes = ['npk', 'nfu']\n",
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(pdir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return globalFlags{config: cfgPath}
}

func TestExpandSetRefsFlattensBundle(t *testing.T) {
	got, err := expandSetRefs(setFixture(t), []string{"@workstation"})
	if err != nil {
		t.Fatal(err)
	}
	if want := "npk totality nfu"; strings.Join(got, " ") != want {
		t.Errorf("@workstation = %v, want %q", got, want)
	}
}

func TestExpandSetRefsMixesAndDedupes(t *testing.T) {
	// A plain input alongside a bundle that already covers it: one lock write,
	// each name once, and the bundle's order preserved.
	got, err := expandSetRefs(setFixture(t), []string{"nixpkgs", "@workstation", "npk"})
	if err != nil {
		t.Fatal(err)
	}
	if want := "nixpkgs npk totality nfu"; strings.Join(got, " ") != want {
		t.Errorf("mixed expansion = %v, want %q", got, want)
	}
}

func TestExpandSetRefsUnknownProject(t *testing.T) {
	_, err := expandSetRefs(setFixture(t), []string{"@ghost"})
	if err == nil || !strings.Contains(err.Error(), "ghost") {
		t.Fatalf("an unknown project should error and name it, got %v", err)
	}
}

func TestExpandSetRefsBareSigil(t *testing.T) {
	if _, err := expandSetRefs(setFixture(t), []string{"@"}); err == nil {
		t.Fatal("a bare @ names no project and should error")
	}
}

func TestExpandSetRefsSkipsCatalogWithoutRefs(t *testing.T) {
	// No `@` anywhere: the plain update path must not read the projects
	// directory at all, so a config that does not exist is not an error here.
	g := globalFlags{config: filepath.Join(t.TempDir(), "missing.toml")}
	got, err := expandSetRefs(g, []string{"nixpkgs", "totality"})
	if err != nil {
		t.Fatalf("plain input names must not touch the catalog: %v", err)
	}
	if strings.Join(got, " ") != "nixpkgs totality" {
		t.Errorf("plain names should pass through unchanged, got %v", got)
	}
}
