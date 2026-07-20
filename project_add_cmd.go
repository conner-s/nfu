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

// runProjectAdd creates or edits a project. With no field flags on a terminal it
// opens the TUI builder; otherwise it takes the definition from flags (the
// agent/scripting path). --print emits the resulting TOML instead of writing.
func runProjectAdd(ctx context.Context, g globalFlags, args []string) error {
	cfg, cat, configDir, err := loadCatalog(g)
	if err != nil {
		return err
	}
	var name string
	if len(args) > 0 {
		name = args[0]
	}

	hasFields := g.inputs != "" || g.hosts != "" || len(g.hostArgs) > 0 || g.noLocalSwitch
	if !hasFields && !g.print && isTTY(os.Stdout) && isTTY(os.Stdin) {
		return runProjectAddTUI(ctx, g, cfg, cat, configDir, name)
	}

	// Flag form: name + at least one input required.
	if name == "" {
		return fmt.Errorf("project name required: `nfu project add <name> --inputs a,b [--hosts x,y]`")
	}
	if err := project.ValidateName(name); err != nil {
		return err
	}
	inputs := splitComma(g.inputs)
	if len(inputs) == 0 {
		return fmt.Errorf("at least one input is required (--inputs a,b)")
	}

	p := config.Project{Inputs: inputs, Hosts: splitComma(g.hosts)}
	if g.noLocalSwitch {
		f := false
		p.LocalSwitch = &f
	}
	hostDefs, err := parseHostArgs(g.hostArgs)
	if err != nil {
		return err
	}

	return persistProject(cfg, cat, configDir, name, p, hostDefs, g.print)
}

// runProjectAddTUI opens the interactive builder, seeded with the catalog's
// hosts (and the existing project when editing), then persists the result.
func runProjectAddTUI(ctx context.Context, g globalFlags, cfg *config.Config, cat *project.Catalog, configDir, name string) error {
	client, err := flake.NewFromConfig(cfg, g.flake)
	if err != nil {
		return err
	}
	var known []tui.HostOption
	for hn, h := range cat.Hosts() {
		known = append(known, tui.HostOption{Name: hn, Args: h.DeployArgs})
	}
	start := tui.FormStart{Name: name}
	if name != "" {
		if e, ok := cat.Project(name); ok {
			start = tui.FormStart{
				Name: name, Inputs: e.Project.Inputs, Hosts: e.Project.Hosts,
				LocalSwitch: e.Project.SwitchLocal(), Editing: true,
			}
		}
	}
	res, err := tui.RunProjectForm(ctx, client, known, start, ui.StylesFor(cfg.Theme))
	if err != nil {
		return err
	}
	if !res.Saved {
		fmt.Println(ui.StylesFor(cfg.Theme).Dim.Render("cancelled"))
		return nil
	}
	return persistProject(cfg, cat, configDir, res.Name, res.Project, res.HostDefs, false)
}

// persistProject writes (or, when print is set, emits) a project. Host defs go to
// the shared index and the project file references host names; --print instead
// inlines the resolved host defs into a self-contained block.
func persistProject(cfg *config.Config, cat *project.Catalog, configDir, name string, p config.Project, hostDefs map[string]config.Host, print bool) error {
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

	if err := writeProject(cfg, configDir, name, p, hostDefs); err != nil {
		return err
	}
	fmt.Printf("%s %s  %s\n", s.Good.Render("saved project:"), s.Value.Render(name),
		s.Dim.Render(cfg.ProjectsPath()+"/"+name+".toml"))
	return nil
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
	fmt.Printf("%s %s\n", ui.StylesFor(cfg.Theme).Good.Render("removed project:"), name)
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
