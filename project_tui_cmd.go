package main

import (
	"context"

	"github.com/conner-s/nfu/internal/config"
	"github.com/conner-s/nfu/internal/flake"
	"github.com/conner-s/nfu/internal/project"
	"github.com/conner-s/nfu/internal/tui"
)

// runProjectManager opens the full-screen project manager (bare `nfu project`
// on a terminal): browse, run/dry-run, and edit projects in one place. The TUI
// itself never touches the filesystem — persistence, catalog reloads, and run
// history flow through the callbacks wired here.
func runProjectManager(ctx context.Context, g globalFlags) error {
	cfg, cat, configDir, err := loadCatalog(g)
	if err != nil {
		return err
	}
	client, err := flake.NewFromConfig(cfg, g.flake)
	if err != nil {
		return err
	}
	cur, err := project.ReadCurrent(configDir)
	if err != nil {
		return err
	}
	runs, err := project.LoadRuns(configDir)
	if err != nil {
		return err
	}
	deps := tui.Deps{
		Client:      client,
		Catalog:     cat,
		Current:     cur,
		Runs:        runs,
		Commit:      !g.noCommit, // project runs commit flake.lock by default
		ConfigDir:   configDir,
		ProjectsDir: cfg.ProjectsPath(),
		Persist: func(name string, p config.Project, defs map[string]config.Host) error {
			return writeProject(cfg, configDir, name, p, defs)
		},
		Reload: func() (*project.Catalog, error) {
			_, cat, _, err := loadCatalog(g)
			return cat, err
		},
		SetCurrent: func(name string) error {
			return project.WriteCurrent(configDir, name)
		},
		Record: func(name string, rec project.RunRecord) error {
			return project.RecordRun(configDir, name, rec)
		},
	}
	return tui.RunManager(ctx, deps)
}
