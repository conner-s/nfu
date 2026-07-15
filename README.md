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

## List

```
nfu list             # aligned table of inputs
nfu list --json      # machine-readable (name, type, ref, url, pin, lastModified, follows)
```

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
