# Issue tracker: Forgejo

Issues, specs and pull requests for this repo live on Forgejo at
**`forgejo.kube.internal.conn3r.xyz`**, repo **`connerlab/nfu`**. Use the `fj` CLI (the native
Forgejo client) for everything it covers, and the Forgejo API for what it
doesn't: issue dependencies.

`fj` infers the repo from the git remote when run inside a clone and is already
signed in (`fj whoami`). Pass `-R <remote>` if a clone has several remotes and
the wrong one wins.

## Issues

- **Create**: `fj issue create "<title>" --body "..."`. For multi-line bodies
  write the body to a file and pass `--body-file <path>`; omitting both opens
  `$EDITOR`, which hangs a non-interactive agent.
- **Read**: `fj issue view <n>` for title and body, `fj issue view <n> comments`
  for the thread.
- **List**: `fj issue search --state open`, plus `--labels <a,b>`,
  `--creator <user>`, `--assignee <user>`. `--state` takes `open`, `closed` or
  `all`.
- **Comment**: `fj issue comment <n> "..."`, or `--body-file <path>`.
- **Labels**: `fj issue edit <n> labels --add "<label>"` / `--rm "<label>"`.
- **Manage labels**: `fj repo labels connerlab/nfu view`, and
  `fj repo labels connerlab/nfu create <name> <hex-colour> -d "..."`.
- **Assign**: `fj issue assign <n> <user>`.
- **Close**: `fj issue close <n> --with-msg "..."`.

## Pull requests

Changes land through Forgejo pull requests, never as direct pushes to the
default branch.

- **Open**: `fj pr create "<title>" --base <default-branch> --head <branch> --body-file <path>`.
  Prefix the title with `WIP: ` for a draft. Put `Closes #<n>` in the body so
  Forgejo closes the issue on merge.
- **Read**: `fj pr view <n>`, plus `comments`, `diff`, `files`, `commits`.
- **CI and mergeability**: `fj pr status <n>` (`--wait` blocks until checks finish).
- **Comment**: `fj pr comment <n> "..."`.
- **List**: `fj pr search --state open`.

Merging is the maintainer's call. If pushing isn't permitted in your session,
stop at a committed local branch and say so; don't route around it.

## Output format

`fj` has **no JSON output**. It switches to `--style minimal` automatically in a
pipe. `--style` is a **global** flag, so it goes before the subcommand
(`fj --style minimal issue search`); placed after, `fj` rejects it.

## Forgejo API

Reach for the API when you need structure (counting, joining, anything where
parsing `fj` text would be fragile) and for the operations `fj` lacks.

Base URL: `https://forgejo.kube.internal.conn3r.xyz/api/v1`. `fj` keeps an OAuth token in
`~/.local/share/forgejo-cli/keys.json`. Run any `fj` command first so it
refreshes an expired token, then:

```sh
TOKEN=$(jq -r '.hosts["forgejo.kube.internal.conn3r.xyz"].token' ~/.local/share/forgejo-cli/keys.json)
curl -sS -H "Authorization: Bearer $TOKEN" https://forgejo.kube.internal.conn3r.xyz/api/v1/repos/connerlab/nfu/issues/<n>
```

Never print the token or write it into a file, issue or commit. If the shell
blocks `curl`, fall back to the text conventions below and tell the user; don't
route around the block.

```
GET    /repos/connerlab/nfu/issues?state=open&labels=needs-triage&type=issues
GET    /repos/connerlab/nfu/issues/<n>/comments
GET    /repos/connerlab/nfu/issues/<n>/dependencies     issues that block <n>
POST   /repos/connerlab/nfu/issues/<n>/dependencies     {"owner":"connerlab","repo":"nfu","index":<blocker>}
DELETE /repos/connerlab/nfu/issues/<n>/dependencies     same body
GET    /repos/connerlab/nfu/issues/<n>/blocks           issues that <n> blocks
```

## Blocking edges between tickets

Use Forgejo's **native issue dependencies**: they show in the UI and Forgejo
refuses to close an issue while a blocker is open. Add each edge with the
`dependencies` POST above.

Also keep a readable copy in the body of the blocked issue, because `fj issue
view` doesn't show dependencies:

```markdown
## Blocked by

- #12
- #17
```

When the API is unreachable, the body text is the only record; add the native
edges once it's back. A ticket is unblocked when every blocker is closed.

## Pull requests as a triage surface

**PRs as a request surface: no.** _(Set to `yes` if this repo treats external
PRs as feature requests; `/triage` reads this flag.)_

When set to `yes`, PRs go through the same labels and states as issues, using
the `fj pr` equivalents. Forgejo shares one number space across issues and PRs,
so a bare `#42` may be either: try `fj pr view 42`, then `fj issue view 42`.
Renovate's dependency PRs are never a request surface.

## When a skill says "publish to the issue tracker"

Create a Forgejo issue with `fj issue create`.

## When a skill says "fetch the relevant ticket"

Run `fj issue view <n>` and `fj issue view <n> comments`.

## Wayfinding operations

Used by `/wayfinder`. The **map** is a single issue, and its **child** issues
are the tickets. Forgejo has no sub-issues, so the map's task list holds the
parent–child link.

- **Map**: an issue labelled `wayfinder:map`, holding the Notes /
  Decisions-so-far / Fog body plus a task list of its children
  (`- [ ] #<n>`).
- **Child ticket**: an issue with `Part of #<map>` as the first line of its
  body, labelled `wayfinder:<type>` (`research`/`prototype`/`grilling`/`task`).
  Add it to the map's task list (`fj issue edit <map> body`).
- **Blocking**: native dependencies plus the `## Blocked by` text, as above.
- **Frontier**: the map's open children, minus any with an open blocker
  (`/issues/<n>/dependencies` returns an open issue, or the body lists one) or
  an assignee. First in map order wins.
- **Claim**: `fj issue assign <n> <your-user>` (`fj whoami`) as the session's
  first write.
- **Resolve**: `fj issue comment <n> "<answer>"`, then `fj issue close <n>`,
  then tick the box in the map and append a context pointer (gist + link) to
  its Decisions-so-far.
