package project

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/conner-s/nfu/internal/config"
	"github.com/conner-s/nfu/internal/flake"
)

func TestStateRoundTrip(t *testing.T) {
	dir := t.TempDir()
	// Missing marker → empty, no error.
	if got, err := ReadCurrent(dir); err != nil || got != "" {
		t.Fatalf("ReadCurrent empty: %q, %v", got, err)
	}
	if err := WriteCurrent(dir, "totality"); err != nil {
		t.Fatal(err)
	}
	if got, _ := ReadCurrent(dir); got != "totality" {
		t.Errorf("ReadCurrent = %q, want totality", got)
	}
	// Clearing removes the marker.
	if err := WriteCurrent(dir, ""); err != nil {
		t.Fatal(err)
	}
	if got, _ := ReadCurrent(dir); got != "" {
		t.Errorf("ReadCurrent after clear = %q, want empty", got)
	}
}

func TestStoreSaveScanRemove(t *testing.T) {
	dir := t.TempDir()
	on := false
	p := config.Project{Inputs: []string{"totality"}, Hosts: []string{"Fabricator"}, LocalSwitch: &on, Description: "mesh"}
	if err := Save(dir, "totality", p); err != nil {
		t.Fatalf("Save: %v", err)
	}
	stored, err := Scan(dir)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(stored) != 1 || stored[0].Name != "totality" {
		t.Fatalf("Scan = %+v", stored)
	}
	got := stored[0].Project
	if len(got.Inputs) != 1 || got.Inputs[0] != "totality" || got.SwitchLocal() || got.Description != "mesh" {
		t.Errorf("round-trip project = %+v", got)
	}
	if stored[0].ReadOnly {
		t.Error("a plain file should not be read-only")
	}
	if err := Remove(dir, "totality"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if s, _ := Scan(dir); len(s) != 0 {
		t.Errorf("after Remove, Scan = %+v", s)
	}
	if err := Remove(dir, "totality"); err == nil {
		t.Error("Remove of a missing project should error")
	}
}

func TestScanMissingDir(t *testing.T) {
	s, err := Scan(filepath.Join(t.TempDir(), "nope"))
	if err != nil || s != nil {
		t.Errorf("Scan of missing dir = %+v, %v", s, err)
	}
}

func TestValidateName(t *testing.T) {
	for _, ok := range []string{"totality", "a-b_c.1"} {
		if err := ValidateName(ok); err != nil {
			t.Errorf("ValidateName(%q) = %v", ok, err)
		}
	}
	for _, bad := range []string{"", ".", "..", "a/b", "has space", "sub/dir"} {
		if err := ValidateName(bad); err == nil {
			t.Errorf("ValidateName(%q) should fail", bad)
		}
	}
}

func TestIndexRoundTrip(t *testing.T) {
	dir := t.TempDir()
	ix, err := LoadIndex(dir) // missing → empty
	if err != nil || len(ix.Hosts) != 0 || len(ix.Order) != 0 {
		t.Fatalf("LoadIndex empty: %+v, %v", ix, err)
	}
	ix = &Index{
		Order: []string{"totality", "inference"},
		Hosts: map[string]config.Host{"Fabricator": {Hostname: "192.0.2.10", DeployArgs: []string{"--magic-rollback", "false"}}},
	}
	if err := SaveIndex(dir, ix); err != nil {
		t.Fatal(err)
	}
	got, err := LoadIndex(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Order) != 2 || got.Order[0] != "totality" {
		t.Errorf("Order = %v", got.Order)
	}
	h := got.Hosts["Fabricator"]
	if h.Hostname != "192.0.2.10" || len(h.DeployArgs) != 2 {
		t.Errorf("Hosts[Fabricator] = %+v", h)
	}
}

// buildCatalog is the common fixture: one added project + one declared (config)
// project + a shared host in the index.
func testCatalog(t *testing.T) *Catalog {
	t.Helper()
	cfg := &config.Config{
		Hosts:    map[string]config.Host{"Fabricator": {DeployArgs: []string{"--magic-rollback", "false"}}},
		Projects: map[string]config.Project{"legacy": {Inputs: []string{"nixpkgs"}}},
	}
	ix := &Index{
		Order: []string{"totality"},
		Hosts: map[string]config.Host{"Servitor": {}},
	}
	stored := []Stored{
		{Name: "totality", Project: config.Project{Inputs: []string{"totality"}, Hosts: []string{"Fabricator"}}, ReadOnly: false},
		{Name: "declared", Project: config.Project{Inputs: []string{"npk"}}, ReadOnly: true},
	}
	return BuildCatalog(cfg, ix, stored)
}

func TestCatalogSourcesAndOrder(t *testing.T) {
	c := testCatalog(t)
	if e, ok := c.Project("totality"); !ok || e.Source != SourceAdded {
		t.Errorf("totality entry = %+v, %v", e, ok)
	}
	if e, ok := c.Project("declared"); !ok || e.Source != SourceDeclared {
		t.Errorf("declared entry = %+v, %v", e, ok)
	}
	if e, ok := c.Project("legacy"); !ok || e.Source != SourceDeclared {
		t.Errorf("legacy (config inline) entry = %+v, %v", e, ok)
	}
	// Order: index puts totality first; the rest alphabetical (declared, legacy).
	got := c.List()
	want := []string{"totality", "declared", "legacy"}
	if len(got) != len(want) {
		t.Fatalf("List len = %d (%+v)", len(got), got)
	}
	for i, w := range want {
		if got[i].Name != w {
			t.Errorf("List[%d] = %q, want %q", i, got[i].Name, w)
		}
	}
}

func TestBuildPlanHostArgs(t *testing.T) {
	c := testCatalog(t)

	// Fabricator resolves its deployArgs from the catalog; localSwitch defaults on.
	plan, err := BuildPlan(c, "totality", true)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.LocalSwitch || !plan.Commit {
		t.Errorf("plan flags = switch %v commit %v", plan.LocalSwitch, plan.Commit)
	}
	if len(plan.Hosts) != 1 || plan.Hosts[0].Name != "Fabricator" {
		t.Fatalf("hosts = %+v", plan.Hosts)
	}
	argv := plan.Hosts[0].Argv()
	want := []string{"--magic-rollback", "false", ".#Fabricator"}
	if len(argv) != len(want) {
		t.Fatalf("argv = %v", argv)
	}
	for i := range want {
		if argv[i] != want[i] {
			t.Errorf("argv[%d] = %q, want %q", i, argv[i], want[i])
		}
	}

	// A project deploying to an undefined host → bare deploy (no extra args).
	c2 := BuildCatalog(nil, nil, []Stored{
		{Name: "p", Project: config.Project{Inputs: []string{"x"}, Hosts: []string{"Bare"}}},
	})
	plan, err = BuildPlan(c2, "p", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Hosts[0].Args) != 0 || plan.Hosts[0].Argv()[0] != ".#Bare" {
		t.Errorf("bare host step = %+v", plan.Hosts[0])
	}
}

func TestBuildPlanInlineHostWins(t *testing.T) {
	// An inline hostDef on the project overrides the shared catalog.
	stored := []Stored{{Name: "p", Project: config.Project{
		Inputs:   []string{"x"},
		Hosts:    []string{"Fabricator"},
		HostDefs: map[string]config.Host{"Fabricator": {DeployArgs: []string{"--inline"}}},
	}}}
	cfg := &config.Config{Hosts: map[string]config.Host{"Fabricator": {DeployArgs: []string{"--catalog"}}}}
	plan, err := BuildPlan(BuildCatalog(cfg, nil, stored), "p", false)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Hosts[0].Args[0] != "--inline" {
		t.Errorf("inline host args = %v, want [--inline]", plan.Hosts[0].Args)
	}
}

func TestBuildPlanErrors(t *testing.T) {
	c := BuildCatalog(nil, nil, []Stored{{Name: "empty", Project: config.Project{}}})
	if _, err := BuildPlan(c, "nope", false); err == nil {
		t.Error("unknown project should error")
	}
	if _, err := BuildPlan(c, "empty", false); err == nil {
		t.Error("project with no inputs should error")
	}
}

func TestSaveRefusesReadOnly(t *testing.T) {
	// Simulate a home-manager file: a symlink whose target path contains
	// "/nix/store/". EvalSymlinks needs the target to exist, so create it.
	dir := t.TempDir()
	store := filepath.Join(dir, "nix", "store", "abc-totality.toml")
	if err := os.MkdirAll(filepath.Dir(store), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store, []byte("inputs=[\"totality\"]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	projDir := filepath.Join(dir, "projects")
	if err := os.MkdirAll(projDir, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(projDir, "totality.toml")
	if err := os.Symlink(store, link); err != nil {
		t.Fatal(err)
	}
	if !isReadOnly(link) {
		t.Fatal("symlink into a /nix/store/ path should be read-only")
	}
	if err := Save(projDir, "totality", config.Project{Inputs: []string{"x"}}); err == nil {
		t.Error("Save over a read-only file should refuse")
	}
	if err := Remove(projDir, "totality"); err == nil {
		t.Error("Remove of a read-only file should refuse")
	}
}

// A plan is what nix will be asked to do. A project that names an input nix
// cannot be asked for by name (a lock can hold one called --refresh, and nix
// would read that as an option) gets no plan, whichever way it came by the
// name: its own inputs or a project it includes.
func TestBuildPlanRefusesInputNamesNixCannotUpdate(t *testing.T) {
	cfg := &config.Config{Projects: map[string]config.Project{
		"direct":  {Inputs: []string{"nixpkgs", "--refresh"}},
		"bundled": {Includes: []string{"direct"}},
		"fine":    {Inputs: []string{"nixpkgs", "home-manager"}},
	}}
	cat := BuildCatalog(cfg, &Index{}, nil)
	for _, name := range []string{"direct", "bundled"} {
		_, err := BuildPlan(cat, name, true)
		var inv *flake.InvalidInputError
		if !errors.As(err, &inv) || len(inv.Names) != 1 || inv.Names[0] != "--refresh" {
			t.Errorf("BuildPlan(%s) = %v, want an InvalidInputError for --refresh", name, err)
		}
	}
	if _, err := BuildPlan(cat, "fine", true); err != nil {
		t.Errorf("BuildPlan(fine): %v", err)
	}
}

// A deploy host is the last argument, as `.#<host>`: it starts with a dot
// whatever the host is called, so deploy cannot read it as an option. The
// arguments before it are options on purpose: they are the host's configured
// deployArgs.
func TestHostStepArgvCannotMakeTheHostAnOption(t *testing.T) {
	for _, host := range []string{"Fabricator", "-x", "--ssh-opts=-oProxyCommand=id", ""} {
		argv := HostStep{Name: host, Args: []string{"--magic-rollback", "false"}}.Argv()
		if len(argv) != 3 || argv[2] != ".#"+host {
			t.Errorf("Argv for host %q = %q, want the host last as .#%s", host, argv, host)
		}
	}
}
