package main

import (
	"context"
	"fmt"
	"os"

	"github.com/conner-s/nfu/internal/flake"
	"github.com/conner-s/nfu/internal/project"
	"github.com/conner-s/nfu/internal/ui"
)

// runDeploy deploys the named hosts from the flake root with their catalog
// deploy args, without updating anything first: the deploy step of a project
// run on its own, for a flake whose changes are already committed.
func runDeploy(ctx context.Context, g globalFlags, hosts []string) error {
	if len(hosts) == 0 {
		return fmt.Errorf("`deploy` needs at least one host (e.g. `nfu deploy Fabricator`)")
	}
	cfg, cat, _, err := loadCatalog(g)
	if err != nil {
		return err
	}
	client, err := flake.NewFromConfig(cfg, g.flake)
	if err != nil {
		return err
	}
	s := ui.StylesFor(cfg.Theme)
	steps := project.HostSteps(cat, hosts)
	if g.dryRun {
		for _, h := range steps {
			ui.Fprintf(os.Stdout, "%s %s\n", s.Dim.Render("would run"), s.Value.Render("deploy "+ui.Names(h.Argv(), " ").String()))
		}
		return nil
	}
	_, err = project.Deploy(ctx, client.Root(), steps, s)
	return err
}
