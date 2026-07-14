package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFlakeDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "flake.nix"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte("flakeRoot = \"/a/b\"\ncommit = true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.FlakeRoot != "/a/b" {
		t.Errorf("FlakeRoot = %q", cfg.FlakeRoot)
	}
	if !cfg.Commit {
		t.Errorf("Commit = false, want true")
	}
	if cfg.Path() != path {
		t.Errorf("Path() = %q", cfg.Path())
	}
}

func TestLoadUnknownField(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	os.WriteFile(path, []byte("flakeRoot = \"/a\"\nbogus = 1\n"), 0o644)
	if _, err := Load(path); err == nil {
		t.Error("Load should reject unknown fields")
	}
}

func TestLoadMissing(t *testing.T) {
	_, err := Load(filepath.Join(t.TempDir(), "nope.toml"))
	if !os.IsNotExist(err) {
		t.Errorf("missing config: err = %v, want IsNotExist", err)
	}
}

func TestResolvePrecedence(t *testing.T) {
	flakeDir := writeFlakeDir(t)
	otherDir := writeFlakeDir(t)

	// --flake override wins over config and env.
	t.Setenv("NFU_FLAKE", otherDir)
	got, err := Resolve(&Config{FlakeRoot: otherDir}, flakeDir)
	if err != nil || got != flakeDir {
		t.Errorf("override: got %q, %v; want %q", got, err, flakeDir)
	}

	// config used when no override.
	got, err = Resolve(&Config{FlakeRoot: flakeDir}, "")
	if err != nil || got != flakeDir {
		t.Errorf("config: got %q, %v; want %q", got, err, flakeDir)
	}

	// env used when no override and no config.
	got, err = Resolve(&Config{}, "")
	if err != nil || got != otherDir {
		t.Errorf("env: got %q, %v; want %q", got, err, otherDir)
	}
}

func TestResolveNothingConfigured(t *testing.T) {
	os.Unsetenv("NFU_FLAKE")
	os.Unsetenv("FLAKE")
	if _, err := Resolve(&Config{}, ""); err == nil {
		t.Error("Resolve with nothing configured should error")
	}
}

func TestResolveMissingFlakeNix(t *testing.T) {
	dir := t.TempDir() // no flake.nix
	_, err := Resolve(&Config{FlakeRoot: dir}, "")
	if err == nil || !strings.Contains(err.Error(), "flake.nix") {
		t.Errorf("Resolve on a dir without flake.nix: err = %v", err)
	}
}

func TestResolveFlakeIsFile(t *testing.T) {
	f := filepath.Join(t.TempDir(), "afile")
	os.WriteFile(f, []byte("x"), 0o644)
	if _, err := Resolve(&Config{}, f); err == nil {
		t.Error("Resolve on a file should error")
	}
}

func TestExpandTilde(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if got := expandTilde("~/x"); got != filepath.Join(home, "x") {
		t.Errorf("expandTilde(~/x) = %q", got)
	}
	if got := expandTilde("/abs"); got != "/abs" {
		t.Errorf("expandTilde(/abs) = %q", got)
	}
}

func TestDefaultPathXDG(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/xdg")
	if got := DefaultPath(); got != "/xdg/nfu/config.toml" {
		t.Errorf("DefaultPath() = %q", got)
	}
}
