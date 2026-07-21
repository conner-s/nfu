package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

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
	if g.asJSON {
		results, updErr := client.Update(ctx, names...)
		if results == nil && updErr != nil {
			return updErr // nothing ran (e.g. unknown input): report and stop
		}
		return finishJSON(ctx, g, cfg, client, results, updErr)
	}
	return streamUpdate(ctx, g, cfg, client, names)
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
	if g.asJSON {
		results, updErr := client.UpdateAll(ctx)
		if results == nil && updErr != nil {
			return updErr
		}
		return finishJSON(ctx, g, cfg, client, results, updErr)
	}
	return streamUpdate(ctx, g, cfg, client, nil) // no names: UpdateSeq updates all
}

// streamUpdate is the human-facing update path: inputs move one nix invocation
// at a time (flake.UpdateSeq), each reported the moment its lock write lands —
// a spinner marks the in-flight input on a TTY — then the usual footer.
// --json keeps the single-invocation Update/UpdateAll path: a machine consumer
// has no use for progress and prefers one atomic nix run.
func streamUpdate(ctx context.Context, g globalFlags, cfg *config.Config, client *flake.Client, names []string) error {
	s := ui.StylesFor(cfg.Theme)
	rep := &streamReporter{s: s, root: client.Root(), tty: isTTY(os.Stdout), now: time.Now().Unix()}
	results, updErr := client.UpdateSeq(ctx, rep, names...)
	rep.finish(updErr != nil)
	if results == nil && updErr != nil {
		return updErr // nothing ran (e.g. unknown input): report and stop
	}
	committed, commitMsg, warn := lock.Commit(ctx, client.Root(), results, commitEnabled(g, cfg))
	emitFooter(s, results, committed, commitMsg, warn, updErr)
	return updErr
}

// streamReporter renders UpdateSeq's callbacks as live terminal output: the
// title once the plan is known, a spinner while an input updates (TTY only),
// and each result row as it lands. On a non-TTY it degrades to plain rows
// appearing as they complete.
type streamReporter struct {
	s    ui.Styles
	root string
	tty  bool
	now  int64

	wn      int         // widest planned input name, for column alignment
	sp      *ui.Spinner // animates the in-flight input; nil between inputs
	current string      // the input in flight; "" once its row printed
}

func (r *streamReporter) Plan(names []string) {
	fmt.Print(titleLine(r.s, r.root))
	for _, n := range names {
		r.wn = max(r.wn, utf8.RuneCountInString(n))
	}
}

func (r *streamReporter) Start(name string) {
	r.current = name
	r.sp = r.s.StartSpinner(os.Stdout, r.tty, ui.RenderUpdatingText(r.s, name, r.wn))
}

func (r *streamReporter) Done(res flake.UpdateResult) {
	r.sp.Stop()
	r.sp, r.current = nil, ""
	fmt.Print(ui.RenderResultRow(r.s, res, r.wn, r.now))
}

// finish clears any in-flight spinner; when the run failed mid-input, the
// input that died gets an explicit ✗ row so the streamed list accounts for it.
func (r *streamReporter) finish(failed bool) {
	if r.sp != nil {
		r.sp.Stop()
		r.sp = nil
	}
	if failed && r.current != "" {
		fmt.Print(ui.RenderFailedRow(r.s, r.current, r.wn))
	}
}

// finishJSON commits flake.lock when enabled and emits the machine summary,
// propagating a non-fatal update error (a later input failing after earlier
// ones moved).
func finishJSON(ctx context.Context, g globalFlags, cfg *config.Config, client *flake.Client, results []flake.UpdateResult, updErr error) error {
	committed, commitMsg, warn := lock.Commit(ctx, client.Root(), results, commitEnabled(g, cfg))
	emitJSON(client.Root(), results, committed, commitMsg, warn, updErr)
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

// emitFooter renders the human summary under the streamed result rows. When
// updErr != nil the run stopped early, so the happy "up to date" / "updated N"
// summary is suppressed — main prints the actual error — and only what
// genuinely moved is counted.
func emitFooter(s ui.Styles, results []flake.UpdateResult, committed bool, commitMsg, warn string, updErr error) {
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
