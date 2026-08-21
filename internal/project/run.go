package project

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

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
func Run(ctx context.Context, client *flake.Client, plan Plan, s ui.Styles) (Result, error) {
	var res Result
	root := client.Root()

	// 1. Update the inputs (a single lock write), then commit flake.lock.
	fmt.Print(stepHeader(s, "update", strings.Join(plan.Inputs, " ")))
	results, updErr := client.Update(ctx, plan.Inputs...)
	res.Updates = results
	if results == nil && updErr != nil {
		return res, updErr // nothing ran (e.g. unknown input)
	}
	fmt.Print(ui.RenderResults(s, results, time.Now().Unix()))

	committed, msg, warn := lock.Commit(ctx, root, results, plan.Commit)
	res.Committed, res.CommitMsg, res.Warn = committed, msg, warn
	if committed {
		fmt.Printf("%s %s\n", s.Good.Render("committed flake.lock:"), s.Dim.Render(msg))
	}
	if warn != "" {
		fmt.Fprintln(os.Stderr, s.Warn.Render("! "+warn))
	}
	if updErr != nil {
		return res, updErr // some inputs moved before a later one failed
	}

	// 2. Rebuild the local host.
	if plan.LocalSwitch {
		fmt.Print(stepHeader(s, "nh os switch", root))
		if err := proc.Stream(ctx, root, "nh", "os", "switch", root); err != nil {
			return res, err
		}
		res.Switched = true
	}

	// 3. Deploy each remote host in order.
	for _, h := range plan.Hosts {
		argv := h.Argv()
		fmt.Print(stepHeader(s, "deploy", strings.Join(argv, " ")))
		if err := proc.Stream(ctx, root, "deploy", argv...); err != nil {
			return res, err
		}
		res.Deployed = append(res.Deployed, h.Name)
	}
	return res, nil
}

// stepHeader is the "→ verb detail" banner printed before each pipeline step.
func stepHeader(s ui.Styles, verb, detail string) string {
	return fmt.Sprintf("\n%s %s  %s\n", s.Dim.Render("→"), s.Title.Render(verb), s.Dim.Render(detail))
}

// RenderPlan describes what a run would do, for --dry-run and the confirm prompt.
func RenderPlan(s ui.Styles, plan Plan, root string) string {
	var b strings.Builder
	fmt.Fprint(&b, titleLine(s, plan.Name, root))

	if len(plan.Members) > 1 {
		// A bundle (or a --merge run): say where the one update line came from,
		// so a long input list is attributable without opening the project files.
		fmt.Fprintf(&b, "  %s  %s\n\n",
			s.Label.Render("bundle"), s.Dim.Render(strings.Join(plan.Members, " + ")))
	}

	commit := "no commit"
	if plan.Commit {
		commit = "commit flake.lock"
	}
	fmt.Fprintf(&b, "  %s  %s  %s\n",
		s.Label.Render("update"), s.Value.Render(strings.Join(plan.Inputs, " ")), s.Dim.Render("("+commit+")"))

	if plan.LocalSwitch {
		fmt.Fprintf(&b, "  %s  %s\n", s.Label.Render("switch"), s.Value.Render("nh os switch"))
	} else {
		fmt.Fprintf(&b, "  %s  %s\n", s.Label.Render("switch"), s.Dim.Render("skipped (localSwitch = false)"))
	}

	if len(plan.Hosts) == 0 {
		fmt.Fprintf(&b, "  %s  %s\n", s.Label.Render("deploy"), s.Dim.Render("(no hosts)"))
	}
	for _, h := range plan.Hosts {
		fmt.Fprintf(&b, "  %s  %s\n", s.Label.Render("deploy"), s.Value.Render("deploy "+strings.Join(h.Argv(), " ")))
	}
	return b.String()
}

// titleLine renders "<project>  <flake path>".
func titleLine(s ui.Styles, name, root string) string {
	return fmt.Sprintf("%s  %s\n\n", s.Title.Render(name), s.Dim.Render(root))
}
