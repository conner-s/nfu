package project

import (
	"sort"

	"github.com/conner-s/nfu/internal/config"
)

// Source labels where a project definition came from.
const (
	SourceAdded    = "added"    // a mutable per-file definition (from `add`)
	SourceDeclared = "declared" // a read-only file (home-manager) or config.toml inline
)

// Entry is one project as the catalog sees it, tagged with its source.
type Entry struct {
	Name    string
	Project config.Project
	Source  string
}

// Catalog is the merged, read-only view of every project and deploy host from
// all sources: config.toml, the mutable index, and the per-file store. Precedence
// (high→low): a per-file project shadows a config.toml inline one; for hosts,
// an inline project def shadows the index, which shadows config.toml.
type Catalog struct {
	projects map[string]Entry
	hosts    map[string]config.Host // index over config (index wins); inline checked per-project
	order    []string               // preferred display order (from the index)
}

// BuildCatalog merges the config, the index, and the scanned per-file store.
func BuildCatalog(cfg *config.Config, ix *Index, stored []Stored) *Catalog {
	c := &Catalog{projects: map[string]Entry{}, hosts: map[string]config.Host{}}

	if cfg != nil {
		for name, h := range cfg.Hosts {
			c.hosts[name] = h
		}
		for name, p := range cfg.Projects {
			c.projects[name] = Entry{Name: name, Project: p, Source: SourceDeclared}
		}
	}
	if ix != nil {
		for name, h := range ix.Hosts { // the mutable shared catalog wins over config
			c.hosts[name] = h
		}
		c.order = append(c.order, ix.Order...)
	}
	// Per-file projects shadow config.toml inline definitions.
	for _, s := range stored {
		src := SourceAdded
		if s.ReadOnly {
			src = SourceDeclared
		}
		c.projects[s.Name] = Entry{Name: s.Name, Project: s.Project, Source: src}
	}
	return c
}

// Project returns the named project entry.
func (c *Catalog) Project(name string) (Entry, bool) {
	e, ok := c.projects[name]
	return e, ok
}

// List returns every project in display order: index order first (for names that
// exist), then the remainder alphabetically.
func (c *Catalog) List() []Entry {
	seen := map[string]bool{}
	var names []string
	for _, n := range c.order {
		if _, ok := c.projects[n]; ok && !seen[n] {
			names = append(names, n)
			seen[n] = true
		}
	}
	rest := make([]string, 0, len(c.projects))
	for n := range c.projects {
		if !seen[n] {
			rest = append(rest, n)
		}
	}
	sort.Strings(rest)
	names = append(names, rest...)

	out := make([]Entry, 0, len(names))
	for _, n := range names {
		out = append(out, c.projects[n])
	}
	return out
}

// Hosts returns the shared deploy-host catalog (index over config), for callers
// that enumerate known hosts (e.g. the add TUI). Inline project defs are not
// included — they are per-project.
func (c *Catalog) Hosts() map[string]config.Host { return c.hosts }

// HostDef resolves a host name for project p: an inline def on the project wins,
// then the shared catalog (index over config). The second return is false when
// the name is unknown — a bare deploy with no extra args.
func (c *Catalog) HostDef(p config.Project, name string) (config.Host, bool) {
	if h, ok := p.HostDefs[name]; ok {
		return h, true
	}
	if h, ok := c.hosts[name]; ok {
		return h, true
	}
	return config.Host{}, false
}
