# nfu

## Agent skills

### Issue tracker

Forgejo issues and pull requests on the repository's home forge, which is where this checkout's `origin` remote points (`git remote get-url origin`), via the `fj` CLI plus the Forgejo API for issue dependencies. See `docs/agents/issue-tracker.md`.

### Triage labels

The five canonical triage roles, each label string equal to its name. See `docs/agents/triage-labels.md`.

### Domain docs

Single-context: one `CONTEXT.md` plus `docs/adr/` at the repo root. See `docs/agents/domain.md`.
