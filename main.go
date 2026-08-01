// Command nfu is a small flake-input updater — the piece nh leaves out.
//
// Where nh rebuilds a system, nfu keeps updating individual flake inputs one
// command away. Point it at your flake once (persistent config, like nh) and
// then:
//
//	nfu totality        update the `totality` input
//	nfu totality npk    update several inputs in one lock write
//	nfu                 interactive picker (choose inputs to update)
//	nfu all             update every input
//	nfu list            show inputs with their locked pins + age
//
// It shells out to a real `nix flake update`, shows the old→new pin delta, and
// can commit the resulting flake.lock (only that file) for you.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/conner-s/nfu/internal/config"
)

const usage = `nfu — flake update helper (the piece nh is missing)

usage:
  nfu <input> [<input>…]   update one or more flake inputs
  nfu                      interactive picker (choose inputs to update)
  nfu all                  update every input
  nfu list                 show inputs with their locked pins and age
  nfu update <input>…      explicit update verb (use it to target an input
                           whose name collides with a reserved word)

  nfu project                 interactive project manager (browse · run · edit)
  nfu project run [<name>…]   update → nh os switch → deploy, scoped to a project
  nfu project add [<name>]    build a project (TUI, or --inputs/--hosts for scripts)
  nfu project edit [<name>]   change a project in the same TUI, pre-filled
  nfu project use [<name>]    set / show the current project (bare run uses it)
  nfu project remove <name>   delete a project
  nfu project list            show configured projects

flags:
  -f, --flake PATH   flake directory (overrides config and $NFU_FLAKE)
      --config PATH  config file (default: ~/.config/nfu/config.toml)
  -c, --commit       git-commit flake.lock after a successful update
      --no-commit    never commit, even if the config enables it
  -n, --dry-run      show what would change; write nothing
  -y, --yes          skip the project-run confirmation prompt
      --json         machine-readable output
  -h, --help         show this help

project add / edit:
      --inputs a,b       flake inputs the project updates
      --hosts x,y        deploy hosts (deploy-rs nodes)
      --host-args 'H:…'  extra deploy args for host H (repeatable)
      --local-switch     run nh os switch for this project
      --no-local-switch  skip nh os switch for this project
      --print            print the project TOML instead of writing it

  add takes the whole definition from these flags; edit merges them into what is
  already stored, so only the fields you name change. With none of them on a
  terminal both open the TUI — edit pre-filled — and offer to git-commit the
  project file afterwards (-y commits without asking, --no-commit skips it).

note: all / list / ls / update / project / help are reserved as the first word.
To update an input literally named one of those, use ` + "`nfu update <name>`" + `.
`

// globalFlags are shared by every subcommand. The `project add` fields below are
// only meaningful under that verb; they live here because nfu parses one flag
// set for the whole command line (see parseInterspersed).
type globalFlags struct {
	flake    string
	config   string
	commit   bool
	noCommit bool
	dryRun   bool
	asJSON   bool
	yes      bool

	// `project add` / `project edit` definition flags.
	inputs        string
	hosts         string
	hostArgs      stringList
	localSwitch   bool
	noLocalSwitch bool
	print         bool
}

// stringList is a repeatable string flag (e.g. --host-args used more than once).
type stringList []string

func (s *stringList) String() string     { return strings.Join(*s, ",") }
func (s *stringList) Set(v string) error { *s = append(*s, v); return nil }

func addFlags(fs *flag.FlagSet, g *globalFlags) {
	fs.StringVar(&g.flake, "flake", "", "flake directory")
	fs.StringVar(&g.flake, "f", "", "flake directory (shorthand)")
	fs.StringVar(&g.config, "config", "", "config file path")
	fs.BoolVar(&g.commit, "commit", false, "commit flake.lock after update")
	fs.BoolVar(&g.commit, "c", false, "commit (shorthand)")
	fs.BoolVar(&g.noCommit, "no-commit", false, "do not commit")
	fs.BoolVar(&g.dryRun, "dry-run", false, "write nothing")
	fs.BoolVar(&g.dryRun, "n", false, "dry-run (shorthand)")
	fs.BoolVar(&g.asJSON, "json", false, "machine-readable JSON")
	fs.BoolVar(&g.yes, "yes", false, "skip the project-run confirmation prompt")
	fs.BoolVar(&g.yes, "y", false, "skip confirmation (shorthand)")

	// `project add` / `project edit` definition flags.
	fs.StringVar(&g.inputs, "inputs", "", "project add/edit: comma-separated flake inputs")
	fs.StringVar(&g.hosts, "hosts", "", "project add/edit: comma-separated deploy hosts")
	fs.Var(&g.hostArgs, "host-args", "project add/edit: 'Host:--flag val' deploy args (repeatable)")
	fs.BoolVar(&g.localSwitch, "local-switch", false, "project add/edit: run nh os switch for this project")
	fs.BoolVar(&g.noLocalSwitch, "no-local-switch", false, "project add/edit: skip nh os switch for this project")
	fs.BoolVar(&g.print, "print", false, "project add/edit: print the project TOML instead of writing")
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	fs := flag.NewFlagSet("nfu", flag.ContinueOnError)
	// Own all usage/error output ourselves: stdlib flag would otherwise print
	// usage to fs's output AND we'd print it again, duplicating help.
	fs.SetOutput(io.Discard)
	var g globalFlags
	addFlags(fs, &g)

	// Allow flags to appear after positionals (stdlib flag stops at the first
	// positional), so `nfu totality --commit` works.
	positionals, err := parseInterspersed(fs, os.Args[1:])
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Print(usage) // -h/--help: usage to stdout, exactly once
			return
		}
		fmt.Fprintf(os.Stderr, "nfu: %v\n\n%s", err, usage)
		os.Exit(2)
	}

	if err := dispatch(ctx, g, positionals); err != nil {
		fail(g, err)
	}
}

// fail reports a fatal error and exits 1. In --json mode it emits a JSON error
// object to stdout so a machine consumer never sees non-JSON on the failure
// path; otherwise it prints the conventional `nfu: <err>` to stderr.
func fail(g globalFlags, err error) {
	if g.asJSON {
		writeJSON(struct {
			Error string `json:"error"`
		}{Error: err.Error()})
	} else {
		fmt.Fprintln(os.Stderr, "nfu: "+err.Error())
	}
	os.Exit(1)
}

// dispatch routes positionals to a command. The reserved first words
// `list`/`ls`, `all`, `update`, and `help` are commands; anything else is
// treated as input names to update. `nfu update <name…>` is the explicit escape
// hatch for updating an input whose name collides with a reserved word. With no
// positionals it opens the interactive picker (or lists, when stdout is not a
// terminal).
func dispatch(ctx context.Context, g globalFlags, positionals []string) error {
	if len(positionals) == 0 {
		return runInteractive(ctx, g)
	}
	switch positionals[0] {
	case "help":
		fmt.Print(usage)
		return nil
	case "list", "ls":
		return runList(ctx, g)
	case "all":
		if len(positionals) > 1 {
			return fmt.Errorf("`all` takes no further arguments")
		}
		return runUpdateAll(ctx, g)
	case "update":
		names := positionals[1:]
		if len(names) == 0 {
			return fmt.Errorf("`update` needs at least one input name (e.g. `nfu update totality`)")
		}
		return runUpdate(ctx, g, names)
	case "project":
		return runProject(ctx, g, positionals[1:])
	default:
		return runUpdate(ctx, g, positionals)
	}
}

// loadConfig loads the config file. A missing DEFAULT config is not an error
// (the flake can come from --flake or $NFU_FLAKE); a missing EXPLICIT --config
// is. Other parse errors always surface.
func loadConfig(path string) (*config.Config, error) {
	explicit := path != ""
	if path == "" {
		path = config.DefaultPath()
	}
	cfg, err := config.Load(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			if explicit {
				return nil, fmt.Errorf("no config at %s", path)
			}
			return &config.Config{}, nil
		}
		return nil, err
	}
	return cfg, nil
}

// commitEnabled resolves the effective commit decision: --no-commit wins, then
// --commit, then the config's default.
func commitEnabled(g globalFlags, cfg *config.Config) bool {
	if g.noCommit {
		return false
	}
	if g.commit {
		return true
	}
	return cfg.Commit
}

// parseInterspersed parses a flag set tolerating flags that appear AFTER
// positional arguments (stdlib flag stops at the first positional), and honors a
// `--` terminator: everything after the first `--` is taken verbatim as
// positionals, so dash-prefixed literals are never re-interpreted as flags.
func parseInterspersed(fs *flag.FlagSet, args []string) ([]string, error) {
	var literal []string
	for i, a := range args {
		if a == "--" {
			literal = append(literal, args[i+1:]...)
			args = args[:i]
			break
		}
	}
	var positionals []string
	for len(args) > 0 {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		rest := fs.Args()
		if len(rest) == 0 {
			break
		}
		positionals = append(positionals, rest[0])
		args = rest[1:]
	}
	return append(positionals, literal...), nil
}
