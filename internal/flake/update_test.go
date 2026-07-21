package flake

import (
	"errors"
	"testing"
)

func TestParseUpdatedHeader(t *testing.T) {
	cases := []struct {
		line string
		name string
		ok   bool
	}{
		{"• Updated input 'totality':", "totality", true},
		{"warning: updating lock file", "", false},
		{"Updated input 'npk':", "npk", true},
		{"  → 'git+ssh://…?rev=abc' (2026-01-01)", "", false},
		{"* Updated input 'nixpkgs-unstable':", "nixpkgs-unstable", true},
	}
	for _, c := range cases {
		name, ok := parseUpdatedHeader(c.line)
		if ok != c.ok || name != c.name {
			t.Errorf("parseUpdatedHeader(%q) = %q,%v; want %q,%v", c.line, name, ok, c.name, c.ok)
		}
	}
}

func TestUpdatedBlocks(t *testing.T) {
	stderr := `warning: Git tree '/home/co/Projects/nixos-config' is dirty
• Updated input 'totality':
    'git+ssh://git@example.com/totality.git?ref=main&rev=6bdb38a' (2026-07-09)
  → 'git+ssh://git@example.com/totality.git?ref=main&rev=a1b2c3d' (2026-07-14)
• Updated input 'npk':
    'git+ssh://git@example.com/npk.git?ref=main&rev=0f9e1ac' (2026-07-12)
  → 'git+ssh://git@example.com/npk.git?ref=main&rev=1122334' (2026-07-14)
`
	blocks := updatedBlocks(stderr)
	if len(blocks) != 2 {
		t.Fatalf("got %d blocks, want 2: %+v", len(blocks), blocks)
	}
	tot, ok := blocks["totality"]
	if !ok {
		t.Fatal("no totality block")
	}
	if !contains(tot, "Updated input 'totality':") {
		t.Errorf("totality block missing its header: %q", tot)
	}
	// The block should include the arrow (→) line and not leak into npk's block.
	if !contains(tot, "a1b2c3d") || contains(tot, "npk") {
		t.Errorf("totality block wrong bounds:\n%s", tot)
	}
	if _, ok := blocks["npk"]; !ok {
		t.Error("no npk block")
	}
}

func TestBuildResults(t *testing.T) {
	before := map[string]Input{
		"totality": {Name: "totality", Rev: "old", LastModified: 100},
		"npk":      {Name: "npk", Rev: "same", LastModified: 200},
	}
	after := map[string]Input{
		"totality": {Name: "totality", Rev: "new", LastModified: 300},
		"npk":      {Name: "npk", Rev: "same", LastModified: 200},
	}
	res := buildResults([]string{"totality", "npk"}, before, after, map[string]string{})
	if len(res) != 2 {
		t.Fatalf("got %d results", len(res))
	}
	if !res[0].Moved() || res[0].Unchanged {
		t.Errorf("totality should have moved: %+v", res[0])
	}
	if res[0].OldPin != "old" || res[0].NewPin != "new" {
		t.Errorf("totality pins = %q → %q", res[0].OldPin, res[0].NewPin)
	}
	if res[1].Moved() || !res[1].Unchanged {
		t.Errorf("npk should be unchanged: %+v", res[1])
	}
}

func TestValidateNames(t *testing.T) {
	all := []Input{
		{Name: "nixpkgs"},
		{Name: "mirror", Follows: true},
		{Name: "totality"},
	}
	byName := indexByName(all)

	if err := validateNames([]string{"nixpkgs", "totality"}, byName, all); err != nil {
		t.Errorf("real inputs should validate: %v", err)
	}

	err := validateNames([]string{"nixpkgs", "typo"}, byName, all)
	var unk *UnknownInputError
	if !errors.As(err, &unk) {
		t.Fatalf("unknown input should yield UnknownInputError, got %v", err)
	}
	if unk.Name != "typo" || len(unk.Available) != 3 {
		t.Errorf("UnknownInputError = %+v", unk)
	}

	if err := validateNames([]string{"mirror"}, byName, all); err == nil || !contains(err.Error(), "follows") {
		t.Errorf("follows alias should be rejected with an explanation, got %v", err)
	}
}

// lockInputs must read root/flake.lock straight from disk and agree with the
// nix-metadata view of the same lock (the testdata fixture).
func TestLockInputs(t *testing.T) {
	c := New("testdata")
	got, err := c.lockInputs()
	if err != nil {
		t.Fatalf("lockInputs: %v", err)
	}
	np, ok := got["nixpkgs"]
	if !ok || np.Rev != "8f0500b9660505dc3cb647775fe9a978a74b5283" {
		t.Errorf("nixpkgs from lock file wrong: %+v (present=%v)", np, ok)
	}
	if mir, ok := got["mirror"]; !ok || !mir.Follows || mir.Rev != np.Rev {
		t.Errorf("mirror should resolve through its follows edge: %+v", mir)
	}
}

func TestLockInputsMissingFile(t *testing.T) {
	c := New(t.TempDir())
	if _, err := c.lockInputs(); err == nil {
		t.Error("a missing flake.lock should be an error")
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
