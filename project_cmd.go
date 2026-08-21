package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

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
		fmt.Print(project.RenderPlan(s, p, client.Root()))
	}

	if g.dryRun {
		fmt.Printf("\n%s\n", s.Dim.Render("dry-run: nothing was written"))
		return nil
	}

	if g.noCommit {
		for _, p := range plans {
			if len(p.Hosts) > 0 {
				fmt.Fprintln(os.Stderr, s.Warn.Render("! --no-commit with a deploy: an uncommitted flake.lock may not reach the host"))
				break
			}
		}
	}

	if !g.yes {
		ok, err := confirm(s, "proceed?")
		if err != nil {
			return err
		}
		if !ok {
			fmt.Println(s.Dim.Render("aborted"))
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
		fmt.Print("\n" + runSummary(s, p.Name, res))
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
			fmt.Println(s.Dim.Render("no current project set"))
			return nil
		}
		fmt.Println(cur)
		return nil
	}
	name := args[0]
	if name == "none" || name == "-" {
		if err := project.WriteCurrent(configDir, ""); err != nil {
			return err
		}
		fmt.Println(s.Dim.Render("current project cleared"))
		return nil
	}
	if _, ok := cat.Project(name); !ok {
		return fmt.Errorf("unknown project %q; `nfu project list` shows what is defined", name)
	}
	if err := project.WriteCurrent(configDir, name); err != nil {
		return err
	}
	fmt.Printf("%s %s\n", s.Good.Render("current project:"), s.Value.Render(name))
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
		fmt.Println(s.Dim.Render("no projects yet — `nfu project add <name>` to create one"))
		return nil
	}
	fmt.Print(renderProjectList(s, cat, entries, cur))
	return nil
}

// renderProjectList renders the aligned project table. Every row shows what a
// run would actually move — a bundle's columns come from its flattened includes,
// not from its own (empty) input list.
func renderProjectList(s ui.Styles, cat *project.Catalog, entries []project.Entry, cur string) string {
	var wn int
	for _, e := range entries {
		if n := len(e.Name); n > wn {
			wn = n
		}
	}
	var b strings.Builder
	for _, e := range entries {
		marker := " "
		if e.Name == cur {
			marker = "*"
		}
		inputs, hostList, localSwitch := e.Project.Inputs, e.Project.Hosts, e.Project.SwitchLocal()
		broken := ""
		if r, err := project.Resolve(cat, e.Name); err == nil {
			inputs, hostList, localSwitch = r.Inputs, project.HostNames(r.Hosts), r.LocalSwitch
		} else {
			// A row that will not resolve still lists — with why, so a bad
			// `includes` is visible here instead of only when the run refuses.
			broken = " ! " + err.Error()
		}
		hosts := "—"
		if len(hostList) > 0 {
			hosts = strings.Join(hostList, ", ")
		}
		sw := ""
		if !localSwitch {
			sw = " (no switch)"
		}
		fmt.Fprintf(&b, " %s %s  %s  %s %s %s%s%s\n",
			s.Good.Render(marker),
			s.Value.Render(padName(e.Name, wn)),
			s.Dim.Render("["+e.Source+"]"),
			s.Label.Render(strings.Join(inputs, " ")),
			s.Dim.Render("→"),
			s.Value.Render(hosts),
			s.Dim.Render(sw),
			s.Warn.Render(broken),
		)
	}
	return b.String()
}

func padName(s string, n int) string {
	if len(s) < n {
		return s + strings.Repeat(" ", n-len(s))
	}
	return s
}

// runSummary is the one-line closing report for a completed pipeline.
func runSummary(s ui.Styles, name string, res project.Result) string {
	return fmt.Sprintf("%s %s  %s\n", s.Good.Render("✓"), s.Value.Render(name), s.Dim.Render(summaryParts(res)))
}

// summaryParts condenses a run result to "updated N · switched · deployed …",
// shared by the closing report and the recorded run history.
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
func confirm(s ui.Styles, prompt string) (bool, error) {
	return confirmDefault(s, prompt, false)
}

// confirmDefault is confirm with a chosen answer for a bare Enter: running a
// pipeline defaults to no, committing a project file you just saved to yes.
func confirmDefault(s ui.Styles, prompt string, def bool) (bool, error) {
	if !isTTY(os.Stdin) {
		return false, fmt.Errorf("refusing to run unattended without a terminal; pass -y/--yes to confirm")
	}
	hint := "[y/N]"
	if def {
		hint = "[Y/n]"
	}
	fmt.Printf("\n%s %s ", s.Warn.Render(prompt), hint)
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
