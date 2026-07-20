# nfu — nix flake update

A tiny Go CLI that does the one thing [`nh`](https://github.com/nix-community/nh)
leaves out: **updating individual flake inputs**, without `cd`-ing into your
flake and remembering the exact `nix flake update` incantation.

Point it at your flake once (a persistent config, like `nh`'s), then:

```
nfu home-manager        update the `home-manager` input
nfu home-manager npk    update several inputs in one lock write
nfu                 interactive picker — choose inputs to update
nfu all             update every input
nfu list            show inputs with their locked pins and age
nfu update <name>   explicit update verb (escape hatch for a reserved name)
```

`all`, `list`, `ls`, `update`, and `help` are reserved as the first word; to
update an input literally named one of those, use `nfu update <name>`. A `follows`
alias has no independent lock, so `nfu <alias>` tells you which input to update
instead.

## Why

`nh os switch` rebuilds your system beautifully, but when a flake input moves
(a personal repo you just pushed to, `nixpkgs`, a fork) you still drop to a
shell, `cd ~/Projects/nixos-config`, and run `nix flake update <input>` — then
squint at the output to see what actually changed. `nfu` keeps that one command
away from anywhere and shows you the **old → new pin delta** (with dates), so a
push-then-update loop is `nfu home-manager`.

It is deliberately small: it shells out to the *real* `nix flake update` (no
reimplementation of locking), reads the lock through `nix flake metadata`, and
optionally commits the resulting `flake.lock` for you.

## Configure

Copy [`config.example.toml`](config.example.toml) to `~/.config/nfu/config.toml`
(honoring `XDG_CONFIG_HOME`), or let the home-manager module generate it:

```toml
flakeRoot = "~/Projects/nixos-config"   # the only required setting
commit    = false                       # auto-commit flake.lock after updates
```

The flake root is resolved in order: `--flake PATH` → config `flakeRoot` →
`$NFU_FLAKE` → `$FLAKE` (the `nh` convention). nfu validates the directory
actually contains a `flake.nix` before it runs, so a typo fails fast and clearly.

### Themes

`theme = "<name>"` picks the color palette for every TUI and listing:
`default`, `gruvbox-dark`, `gruvbox-light`, or `muted`. The default is built
from ANSI palette codes so an unthemed nfu inherits your terminal's scheme;
the named themes are truecolor (downsampled on terminals without it). An
unknown name fails at config load with the valid set. Via home-manager, set
`programs.nfu.theme = "gruvbox-dark"` — a typed enum, so a bad value fails at
nix eval time.

## Update

```
nfu home-manager
```

```
nixos-config  /home/user/Projects/nixos-config

  ✓ home-manager  6bdb38a → a1b2c3d  (5 days ago → just now)

updated 1 input
```

Inputs that were already current are reported (`• already current`), and a
mistyped input lists what's available instead of erroring cryptically. Nothing
is written on a no-op. Multiple inputs update in a **single** lock write:

```
nfu home-manager npk claude-vault-tools
```

> **Note.** Flake inputs are shared across every host a flake defines —
> updating `nixpkgs` moves it for all of them, not just the host you have in
> mind. `nfu` shows exactly what changed so there are no surprises.

### Preview first

```
nfu home-manager --dry-run     # current pin + the command that would run; writes nothing
```

### Commit the lock

```
nfu home-manager --commit      # also `git commit` flake.lock (only that file)
```

Produces `flake.lock: update home-manager (6bdb38a → a1b2c3d)`. It stages and
commits **only** `flake.lock` — never other working-tree changes — and never
pushes. Set `commit = true` in the config to make it the default (and
`--no-commit` to opt out per run). If the flake root isn't a git repo, the
update still succeeds and the commit is skipped with a warning.

## Browse & pick: bare `nfu`

Run `nfu` with no arguments for an interactive picker (Bubble Tea): every input
with its type, short pin, and age; `space` to toggle, `a` for all, `enter` to
update the selection in one lock write, `q` to quit. `c` toggles the commit
switch — whether a successful update also commits flake.lock — starting from the
invoked state (`-c` / `--no-commit` / the config default) and overriding it for
that run. `follows` aliases are shown but not selectable (updating them means
updating what they follow). Piped or non-interactive, bare `nfu` falls back to
the read-only `nfu list`.

**Peek upstream without updating.** `f` probes the hovered input against its
upstream and appends the diff to that row — the new short rev with its age and
absolute timestamp (`→ 35d3407  2 hours ago (2026-07-15 11:37)`), or `• up to
date` if the lock already holds the tip. `F` probes every input in one pass. The
probe never writes your `flake.lock`: it re-locks into a throwaway file (`nix
flake update --output-lock-file`), so it shows exactly what a real update would
land. It runs in the background — the picker stays live and each checked row
spins until its answer arrives — so you can keep navigating or start the update.
Because it is a real lock, a moved input is fetched to hash it (a moved `nixpkgs`
is a full source download), which also warms nix's cache for the update that
follows; `F` across a big flake can therefore take a while. Probing is read-only
and orthogonal to selection: press `space` on a row after seeing its diff to
queue it for the actual update.

## List

```
nfu list             # aligned table of inputs
nfu list --json      # machine-readable (name, type, ref, url, pin, lastModified, follows)
```

## Projects: one command to update, switch, and deploy

`nfu` updates a lock; `nh os switch` rebuilds the box; `deploy .#<host>` pushes to
a remote. When you push a personal input, getting the change *running* means all
three. A **project** bundles them behind one command:

```
nfu project run totality        # nfu totality -c → nh os switch → deploy .#Fabricator
```

Alias it to whatever's fastest to type (the design target is `npr`). A project
names one or more **inputs**, whether to rebuild the **local** host, and an
ordered list of deploy **hosts**. It runs fail-fast: a failed update never
switches, and a failed local build never deploys.

```
nfu project                 full-screen project manager (browse · run · edit)
nfu project run [<name>…]   run the pipeline (current project, or the named ones)
nfu project add [<name>]    build a project — a TUI, or --inputs/--hosts for scripts
nfu project use [<name>]    select the current project (bare `run` uses it), or print it
nfu project remove <name>   delete a project
nfu project list            show projects (current marked '*', with their source)
```

### The project manager

Bare `nfu project` on a terminal opens a full-screen manager (piped or `--json`
it stays the plain list). Four views:

- **list** — every project with its step count, target (`local` / `ssh:host`),
  and last run · status. `j/k` move, `/` filters, `enter` opens, `u` sets the
  current project.
- **detail** — the pipeline laid out step by step next to the project's TOML as
  it sits on disk. `h/l` pages between projects.
- **run** — `r` runs the pipeline (or `d` for a dry-run) with a live step
  sidebar, streaming log, and progress bar; `ctrl-c` cancels the in-flight
  child command. A plan that rebuilds the local host first runs `sudo -v`
  outside the TUI so nh's own sudo never prompts into a pipe.
- **edit** — `e` opens the same builder as `project add` (inputs, local switch,
  deploy hosts) without leaving the manager; `E` opens the project file in
  `$EDITOR`; `n` starts a new project.

Run outcomes land in `~/.config/nfu/runs.toml` (best-effort history, written by
both the manager and `nfu project run`), which is where the list's
"last run · status" column comes from.

Like `kubectl` contexts, `use` sets a current project so a bare `nfu project run`
re-runs it; name projects explicitly to override. Every run **previews the plan
and asks to confirm** before it switches or deploys — `-y`/`--yes` skips the
prompt, `--dry-run` prints the plan and stops:

```
nfu project run totality --dry-run
```
```
totality  /home/you/Projects/nixos-config

  update  totality  (commit flake.lock)
  switch  nh os switch
  deploy  deploy --magic-rollback false .#Fabricator
```

Runs commit `flake.lock` by default — a remote deploy only sees git-tracked
files, so an uncommitted lock wouldn't reach the host (`--no-commit` opts out).

### Hosts and deploy args

Hosts are declared once and referenced by name. `deployArgs` are passed to
`deploy` before the `.#<host>` — Fabricator needs `--magic-rollback false`
(its post-activation canary never lands), Servitor needs nothing:

```toml
[hosts.Fabricator]
deployArgs = ["--magic-rollback", "false"]

[hosts.Servitor]         # no deployArgs → bare `deploy .#Servitor`
```

### Where projects live

Projects are **one file each** under `~/.config/nfu/projects/<name>.toml`, so a
project is individually shareable and version-controllable. The directory is the
source of truth (any `*.toml` there is a project); a thin `projects.toml` index
beside it holds the shared host catalog and display order.

- **`nfu project add`** with no field flags on a terminal opens the TUI (name →
  inputs → local switch → hosts); with `--inputs a,b --hosts x,y` it takes the
  definition from flags (the agent/scripting path). `--print` emits a
  self-contained TOML block (host defs inlined) instead of writing — paste it
  into home-manager, or hand it to someone.
- **home-manager** can declare hosts and projects directly (read-only files
  alongside any you `add`):

  ```nix
  programs.nfu = {
    enable = true;
    settings = { flakeRoot = "~/Projects/nixos-config"; commit = false; };
    hosts.Fabricator = { hostname = "192.168.4.10"; deployArgs = [ "--magic-rollback" "false" ]; };
    projects.totality = { inputs = [ "totality" ]; hosts = [ "Fabricator" ]; };
    projects.inference = { inputs = [ "personal-packages" ]; hosts = [ "Fabricator" ]; localSwitch = false; };
  };
  ```

  A home-manager-declared project is read-only (a Nix-store symlink); `nfu
  project remove` refuses it and points you back at your Nix config.

## Non-interactive contract

Every command takes `--json` for scripting, and `--dry-run` never writes:

```
nfu home-manager --json               # {"flake":…, "results":[{"input":"home-manager","changed":true,…}], …}
nfu list --json
nfu home-manager --dry-run --json
```

## Install

```nix
# flake.nix inputs
nfu = {
  url = "git+ssh://git@github.com/conner-s/nfu.git?ref=main";
  inputs.nixpkgs.follows = "nixpkgs";
};

# home-manager
imports = [ inputs.nfu.homeManagerModules.default ];
programs.nfu = {
  enable = true;
  theme = "gruvbox-dark";
  settings = {
    flakeRoot = "~/Projects/nixos-config";
    commit = false;
  };
};
```

Or build directly: `nix build git+ssh://git@github.com/conner-s/nfu.git`.
The first build after a dependency change reports the correct `vendorHash` to
paste into `flake.nix`.

`nix` itself is always taken from the host (never wrapped in), so your features,
substituters, and `nix.conf` apply; `git` is suffixed onto `PATH` only as a
fallback.

## Development

```
go test ./...        # pure unit tests (config, lock parsing, follows, output)
go build ./...
nix develop          # go + gopls + git
```

The Nix layer (`internal/flake`) is a thin, typed wrapper over `nix flake
metadata`/`update`: it runs a real `nix` with an arg slice (**no shell**),
resolves trusted binaries via `exec.LookPath`, and resolves lock nodes through
the `follows` graph correctly (a second `nixpkgs` copy is keyed `nixpkgs_2`, so
a naïve name lookup would report the wrong rev). The CLI and the TUI are thin
consumers of it plus `internal/lock` (the commit orchestration) and
`internal/ui` (rendering).

- Module path: `github.com/conner-s/nfu`. The binary is
  self-contained (everything under `internal/`), so `buildGoModule` never
  fetches the path (it builds from `./.`).
