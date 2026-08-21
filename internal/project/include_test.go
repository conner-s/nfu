package project

import (
	"strings"
	"testing"

	"github.com/conner-s/nfu/internal/config"
)

func boolp(v bool) *bool { return &v }

// bundleCatalog: three leaf projects and a bundle over two of them. `npk` and
// `nfu` both name totality as an input, so the union has to dedupe.
func bundleCatalog(extra ...Stored) *Catalog {
	stored := []Stored{
		{Name: "npk", Project: config.Project{Inputs: []string{"npk", "totality"}, Hosts: []string{"Fabricator"}}},
		{Name: "nfu", Project: config.Project{Inputs: []string{"nfu", "totality"}}},
		{Name: "remote", Project: config.Project{
			Inputs: []string{"nixus"}, Hosts: []string{"Servitor"}, LocalSwitch: boolp(false)}},
		{Name: "workstation", Project: config.Project{Includes: []string{"npk", "nfu"}}},
	}
	cfg := &config.Config{Hosts: map[string]config.Host{
		"Fabricator": {DeployArgs: []string{"--magic-rollback", "false"}},
		"Servitor":   {},
	}}
	return BuildCatalog(cfg, nil, append(stored, extra...))
}

func TestResolveUnionsAndDedupes(t *testing.T) {
	r, err := Resolve(bundleCatalog(), "workstation")
	if err != nil {
		t.Fatal(err)
	}
	// First-appearance order across members, `totality` named once.
	want := []string{"npk", "totality", "nfu"}
	if strings.Join(r.Inputs, " ") != strings.Join(want, " ") {
		t.Errorf("inputs = %v, want %v", r.Inputs, want)
	}
	if len(r.Members) != 3 || r.Members[0] != "workstation" {
		t.Errorf("members = %v, want the bundle first then its members", r.Members)
	}
	// A member's host comes through with the args the catalog gives it.
	if len(r.Hosts) != 1 || r.Hosts[0].Name != "Fabricator" || r.Hosts[0].Args[0] != "--magic-rollback" {
		t.Errorf("hosts = %+v", r.Hosts)
	}
}

func TestResolveLocalSwitchRules(t *testing.T) {
	cases := []struct {
		name string
		p    config.Project
		want bool
		why  string
	}{
		{"plain", config.Project{Inputs: []string{"a"}}, true, "a plain project defaults to switching"},
		{"plain-off", config.Project{Inputs: []string{"a"}, LocalSwitch: boolp(false)}, false, "explicit false wins"},
		{"bundle", config.Project{Includes: []string{"npk"}}, true, "a member that switches turns the bundle on"},
		{"bundle-off", config.Project{Includes: []string{"npk"}, LocalSwitch: boolp(false)}, false,
			"an explicit false on the BUNDLE overrides a member that wants a switch"},
		{"bundle-remote", config.Project{Includes: []string{"remote"}}, false,
			"a bundle over remote-only members needs no local rebuild"},
	}
	for _, c := range cases {
		cat := bundleCatalog(Stored{Name: c.name, Project: c.p})
		r, err := Resolve(cat, c.name)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if r.LocalSwitch != c.want {
			t.Errorf("%s: localSwitch = %v, want %v (%s)", c.name, r.LocalSwitch, c.want, c.why)
		}
	}
}

func TestResolveDetectsCycle(t *testing.T) {
	cat := BuildCatalog(nil, nil, []Stored{
		{Name: "a", Project: config.Project{Inputs: []string{"x"}, Includes: []string{"b"}}},
		{Name: "b", Project: config.Project{Includes: []string{"c"}}},
		{Name: "c", Project: config.Project{Includes: []string{"a"}}},
	})
	_, err := Resolve(cat, "a")
	if err == nil {
		t.Fatal("a → b → c → a should be reported as a cycle, not recursed forever")
	}
	if !strings.Contains(err.Error(), "cycle") || !strings.Contains(err.Error(), "a → b → c → a") {
		t.Errorf("error should name the path, got %q", err)
	}
}

func TestResolveSelfIncludeIsCycle(t *testing.T) {
	cat := BuildCatalog(nil, nil, []Stored{
		{Name: "a", Project: config.Project{Inputs: []string{"x"}, Includes: []string{"a"}}},
	})
	if _, err := Resolve(cat, "a"); err == nil {
		t.Fatal("a project including itself should be a cycle")
	}
}

func TestResolveDiamondIsNotACycle(t *testing.T) {
	// top → {left, right} → shared. Reaching `shared` twice is legal; it just
	// contributes once.
	cat := BuildCatalog(nil, nil, []Stored{
		{Name: "shared", Project: config.Project{Inputs: []string{"s"}}},
		{Name: "left", Project: config.Project{Inputs: []string{"l"}, Includes: []string{"shared"}}},
		{Name: "right", Project: config.Project{Inputs: []string{"r"}, Includes: []string{"shared"}}},
		{Name: "top", Project: config.Project{Includes: []string{"left", "right"}}},
	})
	r, err := Resolve(cat, "top")
	if err != nil {
		t.Fatalf("a diamond must resolve, got %v", err)
	}
	if got := strings.Join(r.Inputs, " "); got != "l s r" {
		t.Errorf("inputs = %q, want %q", got, "l s r")
	}
}

func TestResolveUnknownIncludeErrors(t *testing.T) {
	cat := BuildCatalog(nil, nil, []Stored{
		{Name: "a", Project: config.Project{Includes: []string{"ghost"}}},
	})
	_, err := Resolve(cat, "a")
	if err == nil || !strings.Contains(err.Error(), "ghost") {
		t.Fatalf("an include naming nothing should error and name it, got %v", err)
	}
}

func TestResolveNestedIncludes(t *testing.T) {
	cat := bundleCatalog(Stored{Name: "everything", Project: config.Project{
		Inputs: []string{"nixpkgs"}, Includes: []string{"workstation", "remote"}}})
	r, err := Resolve(cat, "everything")
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(r.Inputs, " "); got != "nixpkgs npk totality nfu nixus" {
		t.Errorf("nested union = %q", got)
	}
	if len(r.Hosts) != 2 || r.Hosts[0].Name != "Fabricator" || r.Hosts[1].Name != "Servitor" {
		t.Errorf("hosts = %+v, want Fabricator then Servitor", r.Hosts)
	}
}

func TestBuildPlanFromBundle(t *testing.T) {
	plan, err := BuildPlan(bundleCatalog(), "workstation", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Inputs) != 3 || !plan.LocalSwitch {
		t.Errorf("bundle plan = %+v", plan)
	}
	if len(plan.Members) != 3 {
		t.Errorf("a bundle plan should carry its members for the preview, got %v", plan.Members)
	}
}

func TestBuildPlanEmptyBundleErrors(t *testing.T) {
	// A bundle whose members have no inputs still has nothing to update.
	cat := BuildCatalog(nil, nil, []Stored{
		{Name: "hollow", Project: config.Project{}},
		{Name: "a", Project: config.Project{Includes: []string{"hollow"}}},
	})
	if _, err := BuildPlan(cat, "a", false); err == nil {
		t.Error("a bundle resolving to no inputs should error")
	}
}

func TestMergePlansFoldsPipelines(t *testing.T) {
	cat := bundleCatalog()
	var plans []Plan
	for _, n := range []string{"npk", "remote", "nfu"} {
		p, err := BuildPlan(cat, n, false)
		if err != nil {
			t.Fatal(err)
		}
		plans = append(plans, p)
	}
	m := MergePlans(plans, true)

	if got := strings.Join(m.Inputs, " "); got != "npk totality nixus nfu" {
		t.Errorf("merged inputs = %q", got)
	}
	if !m.Commit {
		t.Error("merged plan should take the caller's commit decision")
	}
	// npk switches locally, remote does not — one switch covers the run.
	if !m.LocalSwitch {
		t.Error("merged plan should switch when any member does")
	}
	if len(m.Hosts) != 2 || m.Hosts[0].Name != "Fabricator" || m.Hosts[1].Name != "Servitor" {
		t.Errorf("merged hosts = %+v", m.Hosts)
	}
	if m.Name != "npk+remote+nfu" {
		t.Errorf("merged name = %q", m.Name)
	}
	if strings.Join(m.Members, " ") != "npk remote nfu" {
		t.Errorf("merged members = %v — history records against these", m.Members)
	}
}

func TestMergePlansDedupesHostsAcrossPlans(t *testing.T) {
	a := Plan{Name: "a", Inputs: []string{"x"}, Hosts: []HostStep{{Name: "H", Args: []string{"--first"}}}}
	b := Plan{Name: "b", Inputs: []string{"x", "y"}, Hosts: []HostStep{{Name: "H", Args: []string{"--second"}}}}
	m := MergePlans([]Plan{a, b}, false)
	if len(m.Inputs) != 2 {
		t.Errorf("a shared input must be named once, got %v", m.Inputs)
	}
	if len(m.Hosts) != 1 || m.Hosts[0].Args[0] != "--first" {
		t.Errorf("a shared host deploys once with the first plan's args, got %+v", m.Hosts)
	}
}

func TestRecordPlanMarksEveryMember(t *testing.T) {
	dir := t.TempDir()
	plan := Plan{Name: "a+b", Members: []string{"a", "b"}}
	if err := RecordPlan(dir, plan, RunRecord{OK: true, Detail: "updated 2"}); err != nil {
		t.Fatal(err)
	}
	runs, err := LoadRuns(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := runs["a+b"]; ok {
		t.Error("the synthetic merged name must NOT become a history key")
	}
	for _, n := range []string{"a", "b"} {
		if rec, ok := runs[n]; !ok || !rec.OK {
			t.Errorf("member %q should carry the outcome, got %+v (ok=%v)", n, rec, ok)
		}
	}
}
