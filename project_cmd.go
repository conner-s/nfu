package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/conner-s/termsafe"

	"github.com/conner-s/nfu/internal/config"
	"github.com/conner-s/nfu/internal/flake"
	"github.com/conner-s/nfu/internal/project"
	"github.com/conner-s/nfu/internal/ui"
)

// runProject dispatches the `nfu project …` verbs. Bare `nfu project` opens the
// full-screen manager on a terminal (list · run · edit); piped or --json it
// falls back to the plain list.
func runProject(ctx context.Context, g globalFlags, args []string) error {
	if len(args) == 0 {
		if !g.asJSON && isTTY(os.Stdout) && isTTY(os.Stdin) {
			return runProjectManager(ctx, g)
		}
		return runProjectList(ctx, g)
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "run":
		return runProjectRun(ctx, g, rest)
	case "use":
		return runProjectUse(ctx, g, rest)
	case "list", "ls":
		return runProjectList(ctx, g)
	case "add":
		return runProjectAdd(ctx, g, rest)
	case "edit":
		return runProjectEdit(ctx, g, rest)
	case "remove", "rm":
		return runProjectRemove(ctx, g, rest)
	default:
		return fmt.Errorf("unknown project subcommand %q (want: run | add | edit | use | remove | list)", sub)
	}
}

// loadCatalog assembles the merged view of every project: config.toml, the
// mutable index, and the per-file store. It returns the config (for the flake
// root), the catalog, and the config directory (home of the current-project
// marker and the store).
func loadCatalog(g globalFlags) (*config.Config, *project.Catalog, string, error) {
	cfg, err := loadConfig(g.config)
	if err != nil {
		return nil, nil, "", err
	}
	configDir := cfg.Dir()
	ix, err := project.LoadIndex(configDir)
	if err != nil {
		return nil, nil, "", err
	}
	stored, err := project.Scan(cfg.ProjectsPath())
	if err != nil {
		return nil, nil, "", err
	}
	return cfg, project.BuildCatalog(cfg, ix, stored), configDir, nil
}

// runProjectRun runs the pipeline for the named projects, or the current project
// when none are named.
func runProjectRun(ctx context.Context, g globalFlags, names []string) error {
	cfg, cat, configDir, err := loadCatalog(g)
	if err != nil {
		return err
	}
	if len(names) == 0 {
		cur, err := project.ReadCurrent(configDir)
		if err != nil {
			return err
		}
		if cur == "" {
			return fmt.Errorf("no project given and no current project set\n" +
				"  set one with `nfu project use <name>`, or name it: `nfu project run <name>`")
		}
		names = []string{cur}
	}

	client, err := flake.NewFromConfig(cfg, g.flake)
	if err != nil {
		return err
	}
	s := ui.StylesFor(cfg.Theme)
	commit := !g.noCommit // project runs commit flake.lock by default (deploy needs it tracked)

	// Resolve (and validate) every plan before running any of them.
	plans := make([]project.Plan, 0, len(names))
	for _, n := range names {
		p, err := project.BuildPlan(cat, n, commit)
		if err != nil {
			return err
		}
		plans = append(plans, p)
	}

	// --merge folds the named projects into ONE pipeline: a single lock write over
	// the union of their inputs, one `nh os switch`, and each deploy host once.
	// Without it they run back to back, which rebuilds the local host N times to
	// reach the same state — the wrong shape for "update everything the agents
	// touched today". It stays opt-in because the serial form does buy something
	// real: a later project failing cannot un-deploy an earlier one that already
	// landed.
	if g.merge && len(plans) > 1 {
		plans = []project.Plan{project.MergePlans(plans, commit)}
	}

	for _, p := range plans {
		ui.Fprint(os.Stdout, project.RenderPlan(s, p.Text(client.Root())))
	}

	if g.dryRun {
		ui.Fprintf(os.Stdout, "\n%s\n", s.Dim.Render("dry-run: nothing was written"))
		return nil
	}

	if g.noCommit {
		for _, p := range plans {
			if len(p.Hosts) > 0 {
				ui.Fprintln(os.Stderr, s.Warn.Render("! --no-commit with a deploy: an uncommitted flake.lock may not reach the host"))
				break
			}
		}
	}

	if !g.yes {
		ok, err := confirm(s, ui.Words("proceed?"))
		if err != nil {
			return err
		}
		if !ok {
			ui.Fprintln(os.Stdout, s.Dim.Render("aborted"))
			return nil
		}
	}

	for _, p := range plans {
		start := time.Now()
		res, err := project.Run(ctx, client, p, s)
		// Record the outcome either way — the manager TUI and future lists show
		// "last run · status" from this history. Best-effort, never fatal.
		_ = project.RecordPlan(configDir, p, project.RunRecord{
			At: time.Now(), OK: err == nil,
			Elapsed: int64(time.Since(start).Seconds()),
			Detail:  summaryParts(res),
		})
		if err != nil {
			return err
		}
		ui.Fprint(os.Stdout, "\n"+runSummary(s, p.Name, res))
	}
	return nil
}

// runProjectUse sets (or, with no name, prints) the current project.
func runProjectUse(ctx context.Context, g globalFlags, args []string) error {
	cfg, cat, configDir, err := loadCatalog(g)
	if err != nil {
		return err
	}
	s := ui.StylesFor(cfg.Theme)
	if len(args) == 0 {
		cur, err := project.ReadCurrent(configDir)
		if err != nil {
			return err
		}
		if cur == "" {
			ui.Fprintln(os.Stdout, s.Dim.Render("no current project set"))
			return nil
		}
		// The marker file is anyone's to write. A name nfu itself would
		// accept is plain ASCII and prints as it is.
		ui.Fprintln(os.Stdout, ui.Name(cur))
		return nil
	}
	name := args[0]
	if name == "none" || name == "-" {
		if err := project.WriteCurrent(configDir, ""); err != nil {
			return err
		}
		ui.Fprintln(os.Stdout, s.Dim.Render("current project cleared"))
		return nil
	}
	if _, ok := cat.Project(name); !ok {
		return fmt.Errorf("unknown project %q; `nfu project list` shows what is defined", name)
	}
	if err := project.WriteCurrent(configDir, name); err != nil {
		return err
	}
	ui.Fprintf(os.Stdout, "%s %s\n", s.Good.Render("current project:"), s.Value.Render(ui.Name(name).String()))
	return nil
}

// runProjectList shows every configured project with its source and pipeline.
func runProjectList(ctx context.Context, g globalFlags) error {
	cfg, cat, configDir, err := loadCatalog(g)
	if err != nil {
		return err
	}
	cur, _ := project.ReadCurrent(configDir)
	entries := cat.List()

	if g.asJSON {
		type entry struct {
			Name        string   `json:"name"`
			Source      string   `json:"source"`
			Current     bool     `json:"current"`
			Inputs      []string `json:"inputs"`
			Includes    []string `json:"includes,omitempty"`
			Hosts       []string `json:"hosts,omitempty"`
			LocalSwitch bool     `json:"localSwitch"`
			Description string   `json:"description,omitempty"`
			Error       string   `json:"error,omitempty"`
		}
		out := struct {
			Current  string  `json:"current,omitempty"`
			Projects []entry `json:"projects"`
		}{Current: cur}
		for _, e := range entries {
			// `inputs` is the RESOLVED set a run would move — a bundle's own key is
			// empty, and a consumer asking "what does this update" wants the union.
			// `includes` keeps the declaration visible beside it. A project that will
			// not resolve (cycle, missing member) reports the error rather than
			// vanishing from the listing.
			it := entry{
				Name: e.Name, Source: e.Source, Current: e.Name == cur,
				Inputs: e.Project.Inputs, Includes: e.Project.Includes, Hosts: e.Project.Hosts,
				LocalSwitch: e.Project.SwitchLocal(), Description: e.Project.Description,
			}
			if r, err := project.Resolve(cat, e.Name); err == nil {
				it.Inputs, it.LocalSwitch = r.Inputs, r.LocalSwitch
				it.Hosts = project.HostNames(r.Hosts)
			} else {
				it.Error = err.Error()
			}
			out.Projects = append(out.Projects, it)
		}
		return writeJSON(out)
	}

	s := ui.StylesFor(cfg.Theme)
	if len(entries) == 0 {
		ui.Fprintln(os.Stdout, s.Dim.Render("no projects yet — `nfu project add <name>` to create one"))
		return nil
	}
	ui.Fprint(os.Stdout, renderProjectList(s, cat, entries, cur))
	return nil
}

// renderProjectList renders the aligned project table. Every row shows what a
// run would actually move — a bundle's columns come from its flattened includes,
// not from its own (empty) input list.
//
// Names, inputs and hosts come from project files, and the reason a project
// will not resolve quotes them: each is cleaned where its row is put together.
func renderProjectList(s ui.Styles, cat *project.Catalog, entries []project.Entry, cur string) string {
	type row struct {
		marker                             string
		name, inputs, hosts, broken, notes termsafe.Text
		source, sw                         string
	}
	rows := make([]row, 0, len(entries))
	var wn int
	for _, e := range entries {
		r := row{marker: " ", name: ui.Name(e.Name), source: e.Source, hosts: ui.Words("—")}
		if e.Name == cur {
			r.marker = "*"
		}
		inputs, hostList, localSwitch := e.Project.Inputs, e.Project.Hosts, e.Project.SwitchLocal()
		if res, err := project.Resolve(cat, e.Name); err == nil {
			inputs, hostList, localSwitch = res.Inputs, project.HostNames(res.Hosts), res.LocalSwitch
		} else {
			// A row that will not resolve still lists — with why, so a bad
			// `includes` is visible here instead of only when the run refuses.
			r.broken = ui.Linef(" ! %s", err)
		}
		r.inputs = ui.Names(inputs, " ")
		if len(hostList) > 0 {
			r.hosts = ui.Names(hostList, ", ")
		}
		if !localSwitch {
			r.sw = " (no switch)"
		}
		var notes []string
		for _, in := range inputs {
			if n := ui.InputNameNote(in); !n.IsEmpty() {
				notes = append(notes, ui.Linef("input %s: %s", in, n).String())
			}
		}
		for _, h := range hostList {
			if n := ui.HostNameNote(h); !n.IsEmpty() {
				notes = append(notes, ui.Linef("host %s: %s", h, n).String())
			}
		}
		if len(notes) > 0 {
			r.notes = ui.Words(" ! " + strings.Join(notes, "; "))
		}
		wn = max(wn, utf8.RuneCountInString(r.name.String()))
		rows = append(rows, r)
	}
	var b strings.Builder
	for _, r := range rows {
		fmt.Fprintf(&b, " %s %s  %s  %s %s %s%s%s%s\n",
			s.Good.Render(r.marker),
			s.Value.Render(padName(r.name.String(), wn)),
			s.Dim.Render("["+r.source+"]"),
			s.Label.Render(r.inputs.String()),
			s.Dim.Render("→"),
			s.Value.Render(r.hosts.String()),
			s.Dim.Render(r.sw),
			s.Warn.Render(r.broken.String()),
			s.Warn.Render(r.notes.String()),
		)
	}
	return b.String()
}

func padName(s string, n int) string {
	if w := utf8.RuneCountInString(s); w < n {
		return s + strings.Repeat(" ", n-w)
	}
	return s
}

// runSummary is the one-line closing report for a completed pipeline. The
// project's name and the hosts in the summary are cleaned here.
func runSummary(s ui.Styles, name string, res project.Result) string {
	return fmt.Sprintf("%s %s  %s\n", s.Good.Render("✓"), s.Value.Render(ui.Name(name).String()),
		s.Dim.Render(ui.Line(summaryParts(res)).String()))
}

// summaryParts condenses a run result to "updated N · switched · deployed …",
// shared by the closing report and the recorded run history. It is as raw as
// the host names in it: the history file gets it as it is, and whatever draws
// it cleans it.
func summaryParts(res project.Result) string {
	moved := 0
	for _, r := range res.Updates {
		if r.Moved() {
			moved++
		}
	}
	parts := []string{fmt.Sprintf("updated %d", moved)}
	if res.Switched {
		parts = append(parts, "switched")
	}
	if len(res.Deployed) > 0 {
		parts = append(parts, "deployed "+strings.Join(res.Deployed, ", "))
	}
	return strings.Join(parts, " · ")
}

// stdin is shared by every prompt: a fresh bufio.Reader per question would drop
// whatever the previous read buffered past its newline.
var stdin = bufio.NewReader(os.Stdin)

// confirm asks the user to approve a destructive run. Without a TTY it refuses
// rather than proceed unattended — pass -y for that.
func confirm(s ui.Styles, prompt termsafe.Text) (bool, error) {
	return confirmDefault(s, prompt, false)
}

// confirmDefault is confirm with a chosen answer for a bare Enter: running a
// pipeline defaults to no, committing a project file you just saved to yes.
// The prompt can name files and a repository, so it is a termsafe.Text.
func confirmDefault(s ui.Styles, prompt termsafe.Text, def bool) (bool, error) {
	if !isTTY(os.Stdin) {
		return false, fmt.Errorf("refusing to run unattended without a terminal; pass -y/--yes to confirm")
	}
	hint := "[y/N]"
	if def {
		hint = "[Y/n]"
	}
	ui.Fprintf(os.Stdout, "\n%s %s ", s.Warn.Render(prompt.String()), hint)
	line, err := stdin.ReadString('\n')
	if err != nil && err != io.EOF {
		return false, err
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true, nil
	case "":
		return def, nil
	default:
		return false, nil
	}
}
