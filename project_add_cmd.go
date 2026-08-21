package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/conner-s/nfu/internal/config"
	"github.com/conner-s/nfu/internal/flake"
	"github.com/conner-s/nfu/internal/project"
	"github.com/conner-s/nfu/internal/tui"
	"github.com/conner-s/nfu/internal/ui"
)

// runProjectAdd creates a project. With no field flags on a terminal it opens
// the TUI builder; otherwise it takes the definition from flags (the
// agent/scripting path). --print emits the resulting TOML instead of writing.
// Naming an existing project replaces its definition — `nfu project edit` is the
// verb that keeps what it does not touch.
func runProjectAdd(ctx context.Context, g globalFlags, args []string) error {
	cfg, cat, configDir, err := loadCatalog(g)
	if err != nil {
		return err
	}
	var name string
	if len(args) > 0 {
		name = args[0]
	}

	if !hasFieldFlags(g) && !g.print && isTTY(os.Stdout) && isTTY(os.Stdin) {
		return runProjectForm(ctx, g, cfg, cat, configDir, tui.FormStart{Name: name})
	}

	// Flag form: name + at least one input required.
	if name == "" {
		return fmt.Errorf("project name required: `nfu project add <name> --inputs a,b [--hosts x,y]`, or bundle others with --includes")
	}
	if err := project.ValidateName(name); err != nil {
		return err
	}
	p, hostDefs, err := applyFieldFlags(g, config.Project{})
	if err != nil {
		return err
	}
	if len(p.Inputs) == 0 && len(p.Includes) == 0 {
		return fmt.Errorf("a project needs inputs of its own or projects to bundle\n" +
			"  --inputs a,b        the flake inputs it updates\n" +
			"  --includes x,y      other projects whose inputs it absorbs")
	}
	return persistProject(ctx, g, cfg, cat, configDir, name, p, hostDefs, g.print)
}

// runProjectEdit reopens an existing project in the same builder `add` uses,
// pre-filled with what is on disk — change the inputs, the local switch, the
// hosts, save with ctrl+s, and the write is offered as a git commit. With field
// flags (or off a terminal) it takes the edit from flags instead, merging them
// into the stored definition so only what is named changes.
func runProjectEdit(ctx context.Context, g globalFlags, args []string) error {
	cfg, cat, configDir, err := loadCatalog(g)
	if err != nil {
		return err
	}
	var name string
	if len(args) > 0 {
		name = args[0]
	}
	if name == "" { // bare `edit` edits the current project, like bare `run` runs it
		cur, err := project.ReadCurrent(configDir)
		if err != nil {
			return err
		}
		if cur == "" {
			return fmt.Errorf("no project given and no current project set\n" +
				"  name it: `nfu project edit <name>`, or set one with `nfu project use <name>`")
		}
		name = cur
	}

	e, ok := cat.Project(name)
	if !ok {
		return fmt.Errorf("unknown project %q; `nfu project list` shows what is defined, "+
			"`nfu project add %s` creates it", name, name)
	}
	if e.Source == project.SourceDeclared {
		return fmt.Errorf("project %q is declared read-only (home-manager / config.toml); "+
			"edit it there, or copy it out with `nfu project add %s --print`", name, name)
	}

	if !hasFieldFlags(g) && !g.print {
		if !isTTY(os.Stdout) || !isTTY(os.Stdin) {
			return fmt.Errorf("`project edit` with no field flags opens the builder, which needs a terminal\n" +
				"  name what changes instead: --inputs a,b / --includes x,y / --hosts x,y / --host-args 'H:…' / --local-switch,\n" +
				"  or --print to dump the definition as it stands")
		}
		return runProjectForm(ctx, g, cfg, cat, configDir, tui.EditStart(name, e.Project))
	}

	p, hostDefs, err := applyFieldFlags(g, e.Project)
	if err != nil {
		return err
	}
	return persistProject(ctx, g, cfg, cat, configDir, name, p, hostDefs, g.print)
}

// runProjectForm opens the interactive builder seeded with start (blank for a
// new project, pre-filled for an edit) plus the catalog's known hosts, then
// persists what it returns.
func runProjectForm(ctx context.Context, g globalFlags, cfg *config.Config, cat *project.Catalog, configDir string, start tui.FormStart) error {
	client, err := flake.NewFromConfig(cfg, g.flake)
	if err != nil {
		return err
	}
	var known []tui.HostOption
	for hn, h := range cat.Hosts() {
		known = append(known, tui.HostOption{Name: hn, Args: h.DeployArgs})
	}
	s := ui.StylesFor(cfg.Theme)
	res, err := tui.RunProjectForm(ctx, client, known, start, s)
	if err != nil {
		return err
	}
	if !res.Saved {
		fmt.Println(s.Dim.Render("cancelled"))
		return nil
	}
	return persistProject(ctx, g, cfg, cat, configDir, res.Name, res.Project, res.HostDefs, false)
}

// hasFieldFlags reports whether the command line defined the project itself,
// which is what sends `add`/`edit` down the non-interactive path.
func hasFieldFlags(g globalFlags) bool {
	return g.inputs != "" || g.includes != "" || g.hosts != "" || len(g.hostArgs) > 0 || g.noLocalSwitch || g.localSwitch
}

// applyFieldFlags folds the definition flags into base: only what is named
// changes, so `project edit x --hosts y` keeps x's inputs and description. `add`
// passes an empty base, which makes the same call a plain build-from-flags.
func applyFieldFlags(g globalFlags, base config.Project) (config.Project, map[string]config.Host, error) {
	if g.localSwitch && g.noLocalSwitch {
		return config.Project{}, nil, fmt.Errorf("--local-switch and --no-local-switch contradict each other")
	}
	hostDefs, err := parseHostArgs(g.hostArgs)
	if err != nil {
		return config.Project{}, nil, err
	}

	p := base
	if g.inputs != "" {
		p.Inputs = splitComma(g.inputs)
	}
	if g.includes != "" {
		p.Includes = splitComma(g.includes)
	}
	if g.hosts != "" {
		p.Hosts = splitComma(g.hosts)
		p.HostDefs = pruneHostDefs(p.HostDefs, p.Hosts)
	}
	if g.noLocalSwitch {
		f := false
		p.LocalSwitch = &f
	}
	if g.localSwitch {
		p.LocalSwitch = nil // nil is the default, true
	}
	return p, hostDefs, nil
}

// pruneHostDefs drops the project's inline defs for hosts it no longer deploys,
// so a rewritten host list does not leave dead `[hostDefs.*]` tables behind.
func pruneHostDefs(defs map[string]config.Host, hosts []string) map[string]config.Host {
	if len(defs) == 0 {
		return nil
	}
	out := map[string]config.Host{}
	for _, h := range hosts {
		if d, ok := defs[h]; ok {
			out[h] = d
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// persistProject writes (or, when print is set, emits) a project, then offers to
// commit the files it wrote. Host defs go to the shared index and the project
// file references host names; --print instead inlines the resolved host defs
// into a self-contained block.
func persistProject(ctx context.Context, g globalFlags, cfg *config.Config, cat *project.Catalog, configDir, name string, p config.Project, hostDefs map[string]config.Host, print bool) error {
	s := ui.StylesFor(cfg.Theme)

	if print {
		defs := map[string]config.Host{}
		for _, h := range p.Hosts {
			if d, ok := hostDefs[h]; ok {
				defs[h] = d
			} else if d, ok := cat.HostDef(p, h); ok {
				defs[h] = d
			}
		}
		if len(defs) > 0 {
			p.HostDefs = defs
		}
		data, err := project.MarshalProject(p)
		if err != nil {
			return err
		}
		fmt.Printf("# projects/%s.toml\n%s", name, data)
		return nil
	}

	path := project.FilePath(cfg.ProjectsPath(), name)
	verb := "add"
	if _, err := os.Stat(path); err == nil {
		verb = "update"
	}
	if err := writeProject(cfg, configDir, name, p, hostDefs); err != nil {
		return err
	}
	fmt.Printf("%s %s  %s\n", s.Good.Render("saved project:"), s.Value.Render(name), s.Dim.Render(path))

	offerCommit(ctx, g, s, verb, name, path, project.IndexPath(configDir))
	return nil
}

// offerCommit offers to git-commit the definition files just written, when they
// live in a repo and actually changed. --no-commit skips the offer, -y/--commit
// takes it without asking, and off a terminal it stays silent. A failed commit
// is a warning, never an error — the project is already saved either way.
func offerCommit(ctx context.Context, g globalFlags, s ui.Styles, verb, name string, paths ...string) {
	if g.noCommit {
		return
	}
	auto := g.yes || g.commit
	if !auto && !(isTTY(os.Stdout) && isTTY(os.Stdin)) {
		return
	}
	for _, pend := range project.PendingFiles(ctx, project.CommitMessage(verb, name), paths...) {
		if !auto {
			ok, err := confirmDefault(s, "commit "+pend.Label()+"?", true)
			if err != nil || !ok {
				continue
			}
		}
		if err := pend.Commit(ctx); err != nil {
			fmt.Fprintln(os.Stderr, s.Warn.Render("! commit failed: "+err.Error()))
			continue
		}
		fmt.Printf("%s %s\n", s.Good.Render("committed:"), s.Dim.Render(pend.Message))
	}
}

// writeProject is the silent persistence core shared by `add` and the manager
// TUI: merge new host defs and display order into the index, then write the
// per-file definition.
func writeProject(cfg *config.Config, configDir, name string, p config.Project, hostDefs map[string]config.Host) error {
	ix, err := project.LoadIndex(configDir)
	if err != nil {
		return err
	}
	if len(hostDefs) > 0 {
		if ix.Hosts == nil {
			ix.Hosts = map[string]config.Host{}
		}
		for k, v := range hostDefs {
			ix.Hosts[k] = v
		}
	}
	if !contains(ix.Order, name) {
		ix.Order = append(ix.Order, name)
	}
	if err := project.SaveIndex(configDir, ix); err != nil {
		return err
	}
	return project.Save(cfg.ProjectsPath(), name, p)
}

// runProjectRemove deletes a project file (mutable only) and tidies the index /
// current-project marker.
func runProjectRemove(ctx context.Context, g globalFlags, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("project name required: `nfu project remove <name>`")
	}
	name := args[0]
	cfg, _, configDir, err := loadCatalog(g)
	if err != nil {
		return err
	}
	if err := project.Remove(cfg.ProjectsPath(), name); err != nil {
		return err
	}
	if ix, err := project.LoadIndex(configDir); err == nil {
		ix.Order = removeString(ix.Order, name)
		_ = project.SaveIndex(configDir, ix)
	}
	if cur, _ := project.ReadCurrent(configDir); cur == name {
		_ = project.WriteCurrent(configDir, "")
	}
	s := ui.StylesFor(cfg.Theme)
	fmt.Printf("%s %s\n", s.Good.Render("removed project:"), name)
	offerCommit(ctx, g, s, "remove", name,
		project.FilePath(cfg.ProjectsPath(), name), project.IndexPath(configDir))
	return nil
}

// splitComma splits a comma-separated flag value into trimmed, non-empty parts.
func splitComma(v string) []string {
	var out []string
	for _, p := range strings.Split(v, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// parseHostArgs turns repeated `--host-args 'Name:--flag value'` into host defs.
func parseHostArgs(specs []string) (map[string]config.Host, error) {
	if len(specs) == 0 {
		return nil, nil
	}
	out := map[string]config.Host{}
	for _, spec := range specs {
		name, args, ok := strings.Cut(spec, ":")
		name = strings.TrimSpace(name)
		if !ok || name == "" {
			return nil, fmt.Errorf("bad --host-args %q: want NAME:args (e.g. 'Fabricator:--magic-rollback false')", spec)
		}
		out[name] = config.Host{DeployArgs: strings.Fields(args)}
	}
	return out, nil
}

func contains(ss []string, v string) bool {
	for _, s := range ss {
		if s == v {
			return true
		}
	}
	return false
}

func removeString(ss []string, v string) []string {
	out := ss[:0]
	for _, s := range ss {
		if s != v {
			out = append(out, s)
		}
	}
	return out
}
