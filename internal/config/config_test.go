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

func TestLoadHostsAndProjects(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	toml := `flakeRoot = "/a/b"

[hosts.Fabricator]
hostname   = "192.0.2.10"
deployArgs = ["--magic-rollback", "false"]

[projects.totality]
inputs      = ["totality"]
hosts       = ["Fabricator"]
localSwitch = false
`
	if err := os.WriteFile(path, []byte(toml), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	h, ok := cfg.Hosts["Fabricator"]
	if !ok || h.Hostname != "192.0.2.10" || len(h.DeployArgs) != 2 || h.DeployArgs[0] != "--magic-rollback" {
		t.Errorf("Hosts[Fabricator] = %+v", h)
	}
	p, ok := cfg.Projects["totality"]
	if !ok || len(p.Inputs) != 1 || p.Inputs[0] != "totality" || len(p.Hosts) != 1 {
		t.Errorf("Projects[totality] = %+v", p)
	}
	if p.SwitchLocal() {
		t.Error("localSwitch = false should make SwitchLocal() false")
	}
}

func TestSwitchLocalDefault(t *testing.T) {
	if !(Project{}).SwitchLocal() {
		t.Error("unset localSwitch should default to true")
	}
	on := true
	if !(Project{LocalSwitch: &on}).SwitchLocal() {
		t.Error("localSwitch = true should be true")
	}
}

func TestProjectsPath(t *testing.T) {
	// Default: "projects" beside the config file.
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	os.WriteFile(path, []byte("flakeRoot = \"/a\"\n"), 0o644)
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := cfg.ProjectsPath(), filepath.Join(dir, "projects"); got != want {
		t.Errorf("ProjectsPath() = %q, want %q", got, want)
	}
	// Explicit projectsDir (with ~) wins and is tilde-expanded.
	home := t.TempDir()
	t.Setenv("HOME", home)
	os.WriteFile(path, []byte("flakeRoot = \"/a\"\nprojectsDir = \"~/proj\"\n"), 0o644)
	cfg, err = Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := cfg.ProjectsPath(), filepath.Join(home, "proj"); got != want {
		t.Errorf("ProjectsPath(~) = %q, want %q", got, want)
	}
}

func TestLoadTheme(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	os.WriteFile(path, []byte("flakeRoot = \"/a\"\ntheme = \"gruvbox-dark\"\n"), 0o644)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Theme != "gruvbox-dark" {
		t.Errorf("Theme = %q", cfg.Theme)
	}
}

func TestLoadUnknownTheme(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	os.WriteFile(path, []byte("flakeRoot = \"/a\"\ntheme = \"solarized\"\n"), 0o644)
	_, err := Load(path)
	if err == nil {
		t.Fatal("Load should reject an unknown theme")
	}
	if !strings.Contains(err.Error(), "solarized") || !strings.Contains(err.Error(), "gruvbox-dark") {
		t.Errorf("error should name the bad value and list valid themes, got: %v", err)
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

// The flake root is an argument of `nh os switch <root>`. Resolve makes it
// absolute, so a directory whose name starts with a dash still reaches nh as a
// path and not as an option.
func TestResolveMakesADashLedRootAbsolute(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "-x")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "flake.nix"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(parent)
	got, err := Resolve(&Config{}, "-x")
	if err != nil {
		t.Fatalf("Resolve(-x): %v", err)
	}
	if !filepath.IsAbs(got) || filepath.Base(got) != "-x" {
		t.Errorf("Resolve(-x) = %q, want an absolute path", got)
	}
}
