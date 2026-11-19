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
nfu @workstation    update every input a project bundle covers
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
date` if the lock already holds the tip. `F` probes every input. The probe never
writes your `flake.lock`: it re-locks into a throwaway file (`nix flake update
--output-lock-file`), so it shows exactly what a real update would land.

`F` is **one `nix` invocation per input, up to 8 at once**, starting at the row
under the cursor and wrapping. That matters because nix locks inputs strictly
sequentially inside a single call, and each one costs a live upstream round trip:
asked for 22 inputs at once it pays 22 round trips back to back and can show you
nothing until the last lands (~4.5s on a 22-input flake, versus ~0.8s split
across 8 probes — for identical revs). Split, each row answers on its own, so the
list fills in as the answers arrive, and a remote that is down marks only the
rows that actually depend on it instead of stamping the same error across all 22.

`r` / `R` are the same probe with nix's `--refresh`, which re-resolves ignoring
any cached answer. You rarely need them — an ordinary probe already contacts the
remote — but a force-pushed ref can leave nix holding content it has already
hashed under a name that now means something else.

Because a probe is a real lock, a moved input is fetched to hash it (a moved
`nixpkgs` is a full source download), which also warms nix's cache for the update
that follows. Probing is read-only and orthogonal to selection: press `space` on
a row after seeing its diff to queue it for the actual update.

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
names one or more **inputs** (or other projects to bundle, see below), whether to
rebuild the **local** host, and an ordered list of deploy **hosts**. It runs
fail-fast: a failed update never switches, and a failed local build never
deploys.

```
nfu project                 full-screen project manager (browse · run · edit)
nfu project run [<name>…]   run the pipeline (current project, or the named ones)
nfu project run a b -m      fold several projects into ONE update/switch/deploy
nfu project add [<name>]    build a project — a TUI, or --inputs/--hosts for scripts
nfu project edit [<name>]   change one — the same TUI, pre-filled from the file
nfu project use [<name>]    select the current project (bare `run` uses it), or print it
nfu project remove <name>   delete a project
nfu project list            show projects (current marked '*', with their source)
```

### Bundles: several projects, one rebuild

`nfu project run npk nfu nixus` runs three complete pipelines back to back —
three `nh os switch` rebuilds and three deploy rounds to reach one state. When
several repos moved at once (a few agents working in parallel, a workstation you
just want current), that is the wrong shape. Two ways to get one pipeline:

**Ad hoc — `-m` / `--merge`.** Fold the named projects into a single run: the
union of their inputs in one lock write, one local rebuild, and each deploy host
once, in first-appearance order.

```
nfu project run npk nfu nixus -m --dry-run
```
```
npk+nfu+nixus  /home/you/Projects/nixos-config

  bundle  npk + nfu + nixus

  update  npk totality nfu nixus  (commit flake.lock)
  switch  nh os switch
  deploy  deploy --magic-rollback false .#Fabricator
  deploy  deploy .#Servitor
```

It stays opt-in because the serial form does buy something real: a later project
failing cannot un-deploy an earlier one that already landed.

**Standing — `includes`.** A project defined by the projects it bundles rather
than by inputs of its own:

```toml
# projects/workstation.toml
description = "everything the agents touched"
includes = ['npk', 'nfu', 'nixus']
# inputs = ['nixpkgs']     # optional: extras this bundle adds itself
# hosts  = ['Servitor']    # optional: extra deploy targets
```

```
nfu project add workstation --includes npk,nfu,nixus
nfu project run workstation
```

Editing `npk.toml` changes what `workstation` covers — there is no second copy of
the input list to fall behind. Members may bundle other members; a cycle is a
resolution error naming the path (`include cycle: a → b → a`), never a hang, and
a diamond (two members bundling the same project) contributes once. How the union
resolves:

- **inputs** — deduplicated, first-appearance order, the bundle's own first.
- **hosts** — deduplicated and deployed once, with the deploy args of whichever
  project named the host (so a member's inline `hostDefs` still apply).
- **localSwitch** — an explicit setting on the bundle always wins; otherwise it
  is on when any member switches locally. A bundle over remote-only projects
  needs no local rebuild and does not do one.

**Lock only — `nfu @<project>`.** The same expansion at the update layer: move
the inputs and stop — no switch, no deploy.

```
nfu @workstation            # = nfu npk totality nfu nixus, in one lock write
nfu @workstation nixpkgs    # mix with plain input names; each is named once
```

The `@` sigil is what keeps this clear of the reserved-word problem: no flake
input can be called `@workstation`, so nothing new is reserved.

Run history records against every project a merged run actually covered rather
than the synthetic `npk+nfu+nixus` name, so each member's "last run · status"
column stays true.

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
- **edit** — `e` opens the same pre-filled builder as `nfu project edit` (inputs,
  local switch, deploy hosts) without leaving the manager; `E` opens the project
  file in `$EDITOR`; `n` starts a new project. Saving here writes the file
  without the commit offer — the manager owns the whole screen.

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
  inputs → local switch → hosts); with `--inputs a,b --hosts x,y` — or
  `--includes x,y` for a bundle — it takes the definition from flags (the
  agent/scripting path). `--print` emits a
  self-contained TOML block (host defs inlined) instead of writing — paste it
  into home-manager, or hand it to someone.
- **`nfu project edit <name>`** reopens that same builder **pre-filled** with
  what is on disk — inputs ticked, hosts ticked with their deploy args, the local
  switch as stored. `ctrl+s` saves. Bare `nfu project edit` edits the current
  project. The fields the form does not show (`description`, `flakeRoot`, inline
  host defs) are carried through, and an input the flake no longer has is listed
  as `(not in the flake)` rather than quietly dropped.

  Where `add` replaces a definition, `edit` **merges**: `nfu project edit totality
  --hosts Servitor` changes the hosts and leaves everything else alone. Same
  flags as `add`, plus `--local-switch` to undo a `--no-local-switch`. The form
  does not show `includes` — it is carried through untouched, so editing a
  bundle's hosts in the TUI is safe; change the membership with `--includes` or
  `E` (`$EDITOR`).
- **home-manager** can declare hosts and projects directly (read-only files
  alongside any you `add`):

  ```nix
  programs.nfu = {
    enable = true;
    settings = { flakeRoot = "~/Projects/nixos-config"; commit = false; };
    hosts.Fabricator = { hostname = "192.168.4.10"; deployArgs = [ "--magic-rollback" "false" ]; };
    projects.totality = { inputs = [ "totality" ]; hosts = [ "Fabricator" ]; };
    projects.inference = { inputs = [ "personal-packages" ]; hosts = [ "Fabricator" ]; localSwitch = false; };
    projects.workstation = { includes = [ "totality" "inference" ]; };
  };
  ```

  A home-manager-declared project is read-only (a Nix-store symlink); `nfu
  project remove` and `nfu project edit` refuse it and point you back at your Nix
  config.

### Committing what you saved

A project directory is meant to be version-controlled — that is what
`projectsDir` is for. So when the files `add` / `edit` / `remove` just wrote sit
in a git repo, nfu offers to commit them, and commits **only** those paths:

```
saved project: totality  ~/.config/nfu/projects/totality.toml

commit projects.toml, projects/totality.toml → nixos-config? [Y/n]
committed: nfu: update project totality
```

`-y` takes the offer without asking (for scripts), `--no-commit` skips it, and
off a terminal it stays silent. A failed commit is a warning, never an error —
the project is already saved.

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

Building nfu downloads one Go module from the lab's Forgejo, so the machine
that builds it has to reach `forgejo.kube.internal.conn3r.xyz` the first time
(see [The module from the Forgejo](#the-module-from-the-forgejo)).

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

### Text nfu did not write

A `flake.lock` field, a project file, a path, what nix or deploy printed, what
you typed: none of it is drawn or printed raw. It is cleaned by the
[termsafe](https://forgejo.kube.internal.conn3r.xyz/connerlab/termsafe) module
first, and everything that draws takes a `termsafe.Text`, which only cleaning
can make. [docs/untrusted-text.md](docs/untrusted-text.md) follows every such
value from where it comes in to where it is shown, says what nfu tells you
when cleaning changed something, and lists what gets the raw value on purpose.
A new path to the screen goes into that table and into the entry tables of
`internal/tui/hostile_test.go` and `hostile_test.go`.

`nh os switch` and `deploy` under `nfu project run`, `sudo -v`, and `$EDITOR`
are given the terminal. nfu does not stand between them and the screen and
cannot clean what they write.

### The module from the Forgejo

termsafe lives on the lab's Forgejo, which the public Go proxy cannot see and
whose certificate chains to a private root, `connerlabPKI Root CA`. Two things
follow.

Outside Nix, set `GOPRIVATE=forgejo.kube.internal.conn3r.xyz` before any `go`
command that may touch the network (`go get`, `go mod tidy`, a first `go
build`). Without it go asks proxy.golang.org and sum.golang.org about the
module's path before it tries the Forgejo. `nix develop` sets it. The machine
has to trust the root: go asks the Forgejo over HTTPS where the repository is
and git then clones it over HTTPS.

Inside Nix, `buildGoModule` downloads the modules in a derivation of its own
(`.#nfu.goModules`, the one `vendorHash` is the hash of), with nixpkgs' bundle
of public certificate authorities. `flake.nix` adds the root, from
`nix/connerlab-root-ca.crt`, to that derivation's bundle and sets `GOPRIVATE`
there (`overrideModAttrs`). The version of termsafe is pinned in `go.mod` and
nowhere else. The builder has to reach the Forgejo the first time; after that
the vendor tree is in the store under its hash.

A build that works on your own machine does not show that this holds. Nix
passes the daemon's `NIX_SSL_CERT_FILE` into that derivation, and on a machine
whose own bundle already has the root (nix-darwin with
`security.pki.certificateFiles`) the fetch succeeds with or without the
override. And once the vendor tree is in the store, Nix does not fetch at all.
To see the fetch run, force it:

```
nix build .#nfu.goModules --rebuild     # fetches again and compares
```

Its log names the bundle it used, in a line starting `go modules:`. If the
fetch fails, the error says which of the two requests it was: `Get
"https://forgejo…?go-get=1": … x509` is go not trusting the server, and `git
ls-remote … SSL certificate` is git not trusting it.
