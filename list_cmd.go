package main

import (
	"context"
	"os"
	"time"

	"github.com/conner-s/nfu/internal/tui"
	"github.com/conner-s/nfu/internal/ui"
)

// runList shows every input with its locked pin and age.
func runList(ctx context.Context, g globalFlags) error {
	client, cfg, err := newClient(g)
	if err != nil {
		return err
	}
	inputs, err := client.Inputs(ctx)
	if err != nil {
		return err
	}
	if g.asJSON {
		type entry struct {
			Name         string `json:"name"`
			Type         string `json:"type,omitempty"`
			Ref          string `json:"ref,omitempty"`
			URL          string `json:"url,omitempty"`
			Pin          string `json:"pin,omitempty"`
			LastModified int64  `json:"lastModified,omitempty"`
			Follows      bool   `json:"follows,omitempty"`
		}
		out := struct {
			Flake  string  `json:"flake"`
			Inputs []entry `json:"inputs"`
		}{Flake: client.Root()}
		for _, in := range inputs {
			out.Inputs = append(out.Inputs, entry{in.Name, in.Type, in.Ref, in.URL, in.Pin(), in.LastModified, in.Follows})
		}
		return writeJSON(out)
	}
	s := ui.StylesFor(cfg.Theme)
	ui.Fprint(os.Stdout, titleLine(s, client.Root()))
	ui.Fprint(os.Stdout, ui.RenderInputList(s, ui.InputRows(inputs), time.Now().Unix()))
	return nil
}

// runInteractive opens the picker TUI. When output is not a terminal (piped,
// --json) or --dry-run is set, it falls back to the read-only listing so the
// command is still useful in scripts and previews.
func runInteractive(ctx context.Context, g globalFlags) error {
	if g.asJSON || g.dryRun || !isTTY(os.Stdout) || !isTTY(os.Stdin) {
		return runList(ctx, g)
	}
	client, cfg, err := newClient(g)
	if err != nil {
		return err
	}
	return tui.Run(ctx, client, commitEnabled(g, cfg), ui.StylesFor(cfg.Theme))
}

// isTTY reports whether f is an interactive terminal.
func isTTY(f *os.File) bool {
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}
