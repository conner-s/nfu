package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/conner-s/nfu/internal/config"
	"github.com/conner-s/nfu/internal/flake"
	"github.com/conner-s/nfu/internal/lock"
	"github.com/conner-s/nfu/internal/ui"
)

// newClient loads config and builds a flake client for the resolved root.
func newClient(g globalFlags) (*flake.Client, *config.Config, error) {
	cfg, err := loadConfig(g.config)
	if err != nil {
		return nil, nil, err
	}
	client, err := flake.NewFromConfig(cfg, g.flake)
	if err != nil {
		return nil, nil, err
	}
	return client, cfg, nil
}

// runUpdate updates the named inputs.
func runUpdate(ctx context.Context, g globalFlags, names []string) error {
	client, cfg, err := newClient(g)
	if err != nil {
		return err
	}
	if g.dryRun {
		return previewUpdate(ctx, g, cfg, client, names)
	}
	results, updErr := client.Update(ctx, names...)
	if results == nil && updErr != nil {
		return updErr // nothing ran (e.g. unknown input): report and stop
	}
	return finishUpdate(ctx, g, cfg, client, results, updErr)
}

// runUpdateAll updates every input.
func runUpdateAll(ctx context.Context, g globalFlags) error {
	client, cfg, err := newClient(g)
	if err != nil {
		return err
	}
	if g.dryRun {
		inputs, err := client.Inputs(ctx)
		if err != nil {
			return err
		}
		var names []string
		for _, in := range inputs {
			if !in.Follows {
				names = append(names, in.Name)
			}
		}
		return previewUpdate(ctx, g, cfg, client, names)
	}
	results, updErr := client.UpdateAll(ctx)
	if results == nil && updErr != nil {
		return updErr
	}
	return finishUpdate(ctx, g, cfg, client, results, updErr)
}

// finishUpdate renders results, optionally commits flake.lock, and propagates a
// non-fatal update error (a later input failing after earlier ones moved).
func finishUpdate(ctx context.Context, g globalFlags, cfg *config.Config, client *flake.Client, results []flake.UpdateResult, updErr error) error {
	committed, commitMsg, warn := lock.Commit(ctx, client.Root(), results, commitEnabled(g, cfg))

	if g.asJSON {
		emitJSON(client.Root(), results, committed, commitMsg, warn, updErr)
	} else {
		emitText(cfg, client.Root(), results, committed, commitMsg, warn, updErr)
	}
	return updErr
}

// previewUpdate (dry-run) shows the current pins of the requested inputs and the
// command that would run, without touching flake.lock.
func previewUpdate(ctx context.Context, g globalFlags, cfg *config.Config, client *flake.Client, names []string) error {
	now := time.Now().Unix()
	inputs := make([]flake.Input, 0, len(names))
	for _, n := range names {
		in, err := client.InputInfo(ctx, n)
		if err != nil {
			return err
		}
		inputs = append(inputs, in)
	}
	if g.asJSON {
		type dryEntry struct {
			Input        string `json:"input"`
			Pin          string `json:"pin"`
			LastModified int64  `json:"lastModified,omitempty"`
			Follows      bool   `json:"follows,omitempty"`
		}
		out := struct {
			Flake  string     `json:"flake"`
			DryRun bool       `json:"dryRun"`
			Inputs []dryEntry `json:"inputs"`
		}{Flake: client.Root(), DryRun: true}
		for _, in := range inputs {
			out.Inputs = append(out.Inputs, dryEntry{in.Name, in.Pin(), in.LastModified, in.Follows})
		}
		return writeJSON(out)
	}
	s := ui.StylesFor(cfg.Theme)
	fmt.Print(titleLine(s, client.Root()))
	fmt.Print(ui.RenderInputList(s, inputs, now))
	fmt.Printf("\n%s %s\n", s.Dim.Render("would run"),
		s.Value.Render("nix flake update "+strings.Join(names, " ")))
	return nil
}

// emitText renders the human update summary. When updErr != nil the run failed
// (nix aborts atomically), so the happy "up to date" / "updated N" summary is
// suppressed — main prints the actual error — and only what genuinely moved is
// shown.
func emitText(cfg *config.Config, root string, results []flake.UpdateResult, committed bool, commitMsg, warn string, updErr error) {
	s := ui.StylesFor(cfg.Theme)
	now := time.Now().Unix()
	fmt.Print(titleLine(s, root))
	fmt.Print(ui.RenderResults(s, results, now))

	moved := 0
	for _, r := range results {
		if r.Moved() {
			moved++
		}
	}
	fmt.Println()
	switch {
	case updErr != nil:
		fmt.Println(s.Bad.Render("update did not complete"))
	case moved == 0:
		fmt.Println(s.Dim.Render("everything already up to date"))
	default:
		fmt.Println(s.Value.Render(fmt.Sprintf("updated %s", countInputs(moved))))
	}
	if committed {
		fmt.Printf("%s %s\n", s.Good.Render("committed flake.lock:"), s.Dim.Render(commitMsg))
	}
	if warn != "" {
		fmt.Fprintln(os.Stderr, s.Warn.Render("! "+warn))
	}
}

func countInputs(n int) string {
	if n == 1 {
		return "1 input"
	}
	return fmt.Sprintf("%d inputs", n)
}

// titleLine renders "<basename>  <full path>".
func titleLine(s ui.Styles, root string) string {
	return fmt.Sprintf("%s  %s\n\n", s.Title.Render(filepath.Base(root)), s.Dim.Render(root))
}

// --- JSON output ---

type jsonResult struct {
	Input           string `json:"input"`
	OldPin          string `json:"oldPin,omitempty"`
	NewPin          string `json:"newPin,omitempty"`
	OldLastModified int64  `json:"oldLastModified,omitempty"`
	NewLastModified int64  `json:"newLastModified,omitempty"`
	Changed         bool   `json:"changed"`
	Transition      string `json:"transition,omitempty"`
}

func emitJSON(root string, results []flake.UpdateResult, committed bool, commitMsg, warn string, updErr error) {
	out := struct {
		Flake     string       `json:"flake"`
		Results   []jsonResult `json:"results"`
		Committed bool         `json:"committed"`
		Commit    string       `json:"commit,omitempty"`
		Warning   string       `json:"warning,omitempty"`
		Error     string       `json:"error,omitempty"`
	}{Flake: root, Committed: committed, Commit: commitMsg, Warning: warn}
	if updErr != nil {
		out.Error = updErr.Error()
	}
	for _, r := range results {
		out.Results = append(out.Results, jsonResult{
			Input:           r.Input,
			OldPin:          r.OldPin,
			NewPin:          r.NewPin,
			OldLastModified: r.OldMod,
			NewLastModified: r.NewMod,
			Changed:         r.Moved(),
			Transition:      r.Transition,
		})
	}
	writeJSON(out)
}

func writeJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}
