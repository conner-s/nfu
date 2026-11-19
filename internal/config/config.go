// Package config loads nfu's tool configuration — a small, persistent pointer
// to the flake nfu operates on, plus a couple of workflow toggles.
//
// nfu is a focused sibling of nh: where nh rebuilds a system, nfu keeps the
// missing piece — updating individual flake inputs — one command away
// (`nfu totality`). The only thing it truly needs to know is where the flake
// lives; everything else has a sensible default.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	toml "github.com/pelletier/go-toml/v2"

	"github.com/conner-s/nfu/internal/theme"
)

// Config is the whole tool configuration.
type Config struct {
	// FlakeRoot is the directory holding the flake to update (the one with
	// flake.nix / flake.lock). A leading ~ is expanded.
	FlakeRoot string `toml:"flakeRoot"`

	// Commit, when true, makes a successful update also `git commit` the
	// resulting flake.lock (only that file) with a generated message. It is
	// never a push. The --commit / --no-commit flags override it per run.
	Commit bool `toml:"commit"`

	// Theme names the TUI color palette (see internal/theme). Empty means the
	// default theme, which uses ANSI palette codes so nfu inherits the
	// terminal's scheme. Validated at load: a typo fails with the valid set.
	Theme string `toml:"theme"`

	// Charset says what the terminal decodes: "auto" (the default: go by the
	// locale), "utf8" or "ascii". Where the terminal is not known to decode
	// UTF-8, nfu draws ASCII only. The --charset flag overrides it per run.
	// Validated at load.
	Charset string `toml:"charset"`

	// ProjectsDir is where the per-file project definitions live. A leading ~ is
	// expanded; when unset it defaults to the "projects" directory beside this
	// config file (see ProjectsDir). Point it at a git-tracked directory to
	// version-control your projects.
	ProjectsDir string `toml:"projectsDir"`

	// Hosts is the declarative deploy-host catalog: a deploy target's name mapped
	// to its deploy-rs settings (extra `deployArgs`, an informational hostname).
	// Projects reference hosts by name; the mutable index and inline project defs
	// can extend it (see internal/project).
	Hosts map[string]Host `toml:"hosts"`

	// Projects are declarative pipeline definitions keyed by name. Most projects
	// live as per-file entries under ProjectsDir; this map is the freeform escape
	// hatch for declaring them inline in config.toml.
	Projects map[string]Project `toml:"projects"`

	// path is where this config was loaded from (for error messages); empty if
	// the config came from flags/env alone.
	path string
}

// Host is one deploy target's deploy-rs configuration.
type Host struct {
	// Hostname is informational (documentation); deploy-rs takes the address
	// from the flake's deploy.nodes.<name>. It is kept so a project file/index
	// records where a host lives.
	Hostname string `toml:"hostname,omitempty"`

	// DeployArgs are extra arguments passed to `deploy` before the `.#<host>`
	// installable, e.g. ["--magic-rollback", "false"] for a host whose
	// post-activation canary never lands.
	DeployArgs []string `toml:"deployArgs,omitempty"`
}

// Project is one update→switch→deploy pipeline definition.
type Project struct {
	// Inputs are the flake inputs this project updates. A project needs at least
	// one input OR one entry in Includes.
	Inputs []string `toml:"inputs,omitempty"`

	// Includes names other projects whose definitions this one absorbs: a run
	// updates the union of every member's inputs in ONE lock write, switches
	// once, and deploys each host once. It is how a "workstation" bundle stays
	// drift-free — editing a member changes what the bundle updates, with no
	// second copy of its input list to fall behind. Members may include others;
	// a cycle is a resolution error, never a hang.
	Includes []string `toml:"includes,omitempty"`

	// Hosts are the deploy targets to activate after the local switch, in order.
	// Each is a host name resolved through the catalog (Config.Hosts, the index,
	// or an inline HostDefs entry); an unknown name deploys bare (no extra args).
	Hosts []string `toml:"hosts,omitempty"`

	// LocalSwitch controls whether a run rebuilds the local host (`nh os switch`).
	// nil means the default, true; set false for a remote-only project.
	LocalSwitch *bool `toml:"localSwitch,omitempty"`

	// Description is an optional human note shown in listings.
	Description string `toml:"description,omitempty"`

	// HostDefs are optional inline host definitions carried by a portable/shared
	// project file (the `hosts` key is the reference array, so inline defs live
	// under a distinct `[hostDefs.*]` table). `add` normally writes host defs to
	// the shared index instead; --print inlines them here for a self-contained
	// file.
	HostDefs map[string]Host `toml:"hostDefs,omitempty"`

	// FlakeRoot optionally overrides the flake this project operates on.
	FlakeRoot string `toml:"flakeRoot,omitempty"`
}

// SwitchLocal reports whether a run should rebuild the local host. It defaults
// to true when unset.
func (p Project) SwitchLocal() bool { return p.LocalSwitch == nil || *p.LocalSwitch }

// DefaultPath returns ~/.config/nfu/config.toml (honoring XDG_CONFIG_HOME).
func DefaultPath() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "nfu", "config.toml")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "config.toml"
	}
	return filepath.Join(home, ".config", "nfu", "config.toml")
}

// expandTilde expands a leading ~ to the user's home directory.
func expandTilde(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(p, "~"), "/"))
		}
	}
	return p
}

// Load reads and parses the config at path. A missing file is reported via
// os.IsNotExist(err) so the caller can offer a friendly "create one" hint while
// still allowing --flake / $NFU_FLAKE to stand in for a config.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err // includes fs.ErrNotExist for a missing file
	}
	var c Config
	dec := toml.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&c); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}
	if _, err := theme.Get(c.Theme); err != nil {
		return nil, fmt.Errorf("config %s: %w", path, err)
	}
	switch strings.ToLower(c.Charset) {
	case "", "auto", "utf8", "utf-8", "ascii":
	default:
		return nil, fmt.Errorf("config %s: charset must be auto, utf8 or ascii, not %q", path, c.Charset)
	}
	c.path = path
	c.FlakeRoot = expandTilde(c.FlakeRoot)
	return &c, nil
}

// Path returns where this config was loaded from ("" if none).
func (c *Config) Path() string { return c.path }

// Dir returns the directory holding this config file — the home for nfu's
// mutable sibling state (the current-project marker, the projects index, the
// per-file project store). It falls back to the default config directory when
// the config came from flags/env alone (no file on disk).
func (c *Config) Dir() string {
	if c != nil && c.path != "" {
		return filepath.Dir(c.path)
	}
	return filepath.Dir(DefaultPath())
}

// ProjectsPath returns the directory holding per-file project definitions:
// the configured projectsDir (tilde-expanded) when set, else "projects" beside
// the config file (see Dir).
func (c *Config) ProjectsPath() string {
	if c != nil && c.ProjectsDir != "" {
		return expandTilde(c.ProjectsDir)
	}
	return filepath.Join(c.Dir(), "projects")
}

// Resolve picks the flake root using, in order: an explicit override (the
// --flake flag), the config's flakeRoot, then the $NFU_FLAKE / $FLAKE env vars
// (the nh convention). The result is tilde/relative-expanded to an absolute
// path and validated to contain a flake.nix, so every later nix invocation has
// a real flake to talk to and the error surfaces here rather than deep in nix.
func Resolve(c *Config, override string) (string, error) {
	root, src := pickRoot(c, override)
	if root == "" {
		return "", fmt.Errorf("no flake configured\n"+
			"  set flakeRoot in %s (see config.example.toml),\n"+
			"  or pass --flake PATH, or export NFU_FLAKE=/path/to/flake", DefaultPath())
	}
	root = expandTilde(root)
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve flake path %q (%s): %w", root, src, err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("flake root %q (%s): %w", abs, src, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("flake root %q (%s) is not a directory", abs, src)
	}
	if _, err := os.Stat(filepath.Join(abs, "flake.nix")); err != nil {
		return "", fmt.Errorf("no flake.nix in %q (%s)", abs, src)
	}
	return abs, nil
}

// pickRoot returns the chosen root and a human label for where it came from.
func pickRoot(c *Config, override string) (root, source string) {
	if override != "" {
		return override, "--flake"
	}
	if c != nil && c.FlakeRoot != "" {
		return c.FlakeRoot, "config flakeRoot"
	}
	if v := os.Getenv("NFU_FLAKE"); v != "" {
		return v, "$NFU_FLAKE"
	}
	if v := os.Getenv("FLAKE"); v != "" {
		return v, "$FLAKE"
	}
	return "", ""
}
