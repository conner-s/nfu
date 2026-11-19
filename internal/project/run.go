package project

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/conner-s/termsafe"

	"github.com/conner-s/nfu/internal/flake"
	"github.com/conner-s/nfu/internal/lock"
	"github.com/conner-s/nfu/internal/proc"
	"github.com/conner-s/nfu/internal/ui"
)

// Result records what a pipeline run did, for the closing summary / JSON.
type Result struct {
	Updates   []flake.UpdateResult
	Committed bool
	CommitMsg string
	Warn      string
	Switched  bool
	Deployed  []string
}

// Run executes a resolved plan against client's flake root: update the inputs
// (committing flake.lock when enabled), optionally `nh os switch`, then `deploy`
// each host in order. It is fail-fast — a failed update aborts before the switch,
// and a failed switch aborts before any deploy (local build gates remote). The
// long child commands stream straight to the terminal (see internal/proc); this
// function prints only the step headers and the update/commit summary, so its
// output interleaves correctly with theirs.
//
// What nh and deploy write goes to the terminal they were given, uncleaned:
// nfu does not stand between them and the screen. What this function prints
// itself is cleaned (PlanText, ui.ResultRows, ui.Line).
func Run(ctx context.Context, client *flake.Client, plan Plan, s ui.Styles) (Result, error) {
	var res Result
	root := client.Root()
	shown := plan.Text(root)

	// 1. Update the inputs (a single lock write), then commit flake.lock.
	fmt.Print(stepHeader(s, "update", shown.Inputs))
	results, updErr := client.Update(ctx, plan.Inputs...)
	res.Updates = results
	if results == nil && updErr != nil {
		return res, updErr // nothing ran (e.g. unknown input)
	}
	fmt.Print(ui.RenderResults(s, ui.ResultRows(results), time.Now().Unix()))

	committed, msg, warn := lock.Commit(ctx, root, results, plan.Commit)
	res.Committed, res.CommitMsg, res.Warn = committed, msg, warn
	if committed {
		fmt.Printf("%s %s\n", s.Good.Render("committed flake.lock:"), s.Dim.Render(ui.Line(msg).String()))
	}
	if warn != "" {
		fmt.Fprintln(os.Stderr, s.Warn.Render("! "+ui.Line(warn).String()))
	}
	if updErr != nil {
		return res, updErr // some inputs moved before a later one failed
	}

	// 2. Rebuild the local host.
	if plan.LocalSwitch {
		fmt.Print(stepHeader(s, "nh os switch", shown.Root))
		if err := proc.Stream(ctx, root, "nh", "os", "switch", root); err != nil {
			return res, err
		}
		res.Switched = true
	}

	// 3. Deploy each remote host in order.
	for i, h := range plan.Hosts {
		fmt.Print(stepHeader(s, "deploy", shown.Deploys[i]))
		if err := proc.Stream(ctx, root, "deploy", h.Argv()...); err != nil {
			return res, err
		}
		res.Deployed = append(res.Deployed, h.Name)
	}
	return res, nil
}

// stepHeader is the "→ verb detail" banner printed before each pipeline step.
func stepHeader(s ui.Styles, verb string, detail termsafe.Text) string {
	return fmt.Sprintf("\n%s %s  %s\n", s.Dim.Render("→"), s.Title.Render(verb), s.Dim.Render(detail.String()))
}

// PlanText is a Plan as it is drawn: by RenderPlan for --dry-run and the
// confirm prompt, and in the step headers of a run. Everything in a plan comes
// from project files and the command line, so nothing of it is drawn raw; the
// Plan itself stays what nix and deploy are given.
type PlanText struct {
	Name        termsafe.Text
	Root        termsafe.Text
	Members     termsafe.Text // "a + b"; empty unless the plan folds several projects
	Inputs      termsafe.Text
	Commit      bool
	LocalSwitch bool
	Deploys     []termsafe.Text // the arguments of each deploy, in the plan's order
	// Notes name the hosts of the plan that are not what they look like
	// (ui.HostNameNote).
	Notes []termsafe.Text
}

// Text cleans a plan for display. root is the flake it runs against.
func (p Plan) Text(root string) PlanText {
	t := PlanText{
		Name:        ui.Name(p.Name),
		Root:        ui.Line(root),
		Inputs:      ui.Names(p.Inputs, " "),
		Commit:      p.Commit,
		LocalSwitch: p.LocalSwitch,
		Notes:       p.Notes(),
	}
	if len(p.Members) > 1 {
		t.Members = ui.Names(p.Members, " + ")
	}
	for _, h := range p.Hosts {
		t.Deploys = append(t.Deploys, ui.Names(h.Argv(), " "))
	}
	return t
}

// Notes lists the hosts of the plan whose names deserve a second look before
// the plan is run, each with the reason. It is empty for a plan of ordinary
// names. Inputs need no note here: a plan with an input nix cannot be asked
// for by name is never built (BuildPlan).
func (p Plan) Notes() []termsafe.Text {
	var notes []termsafe.Text
	for _, h := range p.Hosts {
		if n := ui.HostNameNote(h.Name); !n.IsEmpty() {
			notes = append(notes, ui.Linef("host %s: %s", h.Name, n))
		}
	}
	return notes
}

// RenderPlan describes what a run would do, for --dry-run and the confirm prompt.
func RenderPlan(s ui.Styles, plan PlanText) string {
	var b strings.Builder
	fmt.Fprint(&b, ui.TitleLine(s, plan.Name, plan.Root))

	if !plan.Members.IsEmpty() {
		// A bundle (or a --merge run): say where the one update line came from,
		// so a long input list is attributable without opening the project files.
		fmt.Fprintf(&b, "  %s  %s\n\n",
			s.Label.Render("bundle"), s.Dim.Render(plan.Members.String()))
	}

	commit := "no commit"
	if plan.Commit {
		commit = "commit flake.lock"
	}
	fmt.Fprintf(&b, "  %s  %s  %s\n",
		s.Label.Render("update"), s.Value.Render(plan.Inputs.String()), s.Dim.Render("("+commit+")"))

	if plan.LocalSwitch {
		fmt.Fprintf(&b, "  %s  %s\n", s.Label.Render("switch"), s.Value.Render("nh os switch"))
	} else {
		fmt.Fprintf(&b, "  %s  %s\n", s.Label.Render("switch"), s.Dim.Render("skipped (localSwitch = false)"))
	}

	if len(plan.Deploys) == 0 {
		fmt.Fprintf(&b, "  %s  %s\n", s.Label.Render("deploy"), s.Dim.Render("(no hosts)"))
	}
	for _, argv := range plan.Deploys {
		fmt.Fprintf(&b, "  %s  %s\n", s.Label.Render("deploy"), s.Value.Render("deploy "+argv.String()))
	}
	for _, n := range plan.Notes {
		fmt.Fprintf(&b, "  %s\n", s.Warn.Render("! "+n.String()))
	}
	return b.String()
}
