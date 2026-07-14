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

	// path is where this config was loaded from (for error messages); empty if
	// the config came from flags/env alone.
	path string
}

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
	c.path = path
	c.FlakeRoot = expandTilde(c.FlakeRoot)
	return &c, nil
}

// Path returns where this config was loaded from ("" if none).
func (c *Config) Path() string { return c.path }

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
