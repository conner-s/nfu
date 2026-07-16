package flake

import (
	"encoding/json"
	"os"
	"testing"
)

func loadFixture(t *testing.T) *metadata {
	t.Helper()
	data, err := os.ReadFile("testdata/metadata.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var md metadata
	if err := json.Unmarshal(data, &md); err != nil {
		t.Fatalf("unmarshal fixture: %v", err)
	}
	return &md
}

func TestBuildInputs(t *testing.T) {
	md := loadFixture(t)
	inputs, err := md.buildInputs()
	if err != nil {
		t.Fatalf("buildInputs: %v", err)
	}

	// Name-sorted.
	want := []string{"hmc", "mirror", "nixpkgs", "totality"}
	if len(inputs) != len(want) {
		t.Fatalf("got %d inputs, want %d: %+v", len(inputs), len(want), inputs)
	}
	for i, w := range want {
		if inputs[i].Name != w {
			t.Errorf("input[%d].Name = %q, want %q", i, inputs[i].Name, w)
		}
	}

	byName := map[string]Input{}
	for _, in := range inputs {
		byName[in.Name] = in
	}

	// totality: its OWN git pin, not the nixpkgs_2 it depends on.
	tot := byName["totality"]
	if tot.Type != "git" {
		t.Errorf("totality.Type = %q, want git", tot.Type)
	}
	if tot.Rev != "6bdb38aa582f3ab09bb61a9fddd4cec7e069eee2" {
		t.Errorf("totality.Rev = %q (wrong node resolved?)", tot.Rev)
	}
	if tot.Ref != "main" {
		t.Errorf("totality.Ref = %q, want main", tot.Ref)
	}
	if tot.LastModified != 1784048328 {
		t.Errorf("totality.LastModified = %d", tot.LastModified)
	}
	if tot.Follows {
		t.Errorf("totality should not be a follows alias")
	}

	// nixpkgs: the ROOT nixpkgs (rev 111…), never accidentally nixpkgs_2 (222…).
	np := byName["nixpkgs"]
	if np.Rev != "1111111111111111111111111111111111111111" {
		t.Errorf("nixpkgs.Rev = %q, want the root nixpkgs (111…)", np.Rev)
	}
	if np.URL != "github:nixos/nixpkgs" {
		t.Errorf("nixpkgs.URL = %q, want synthesized github:nixos/nixpkgs", np.URL)
	}

	// mirror: a root-level follows alias → marked, resolves to nixpkgs' pin.
	mir := byName["mirror"]
	if !mir.Follows {
		t.Errorf("mirror should be a follows alias")
	}
	if mir.Rev != np.Rev {
		t.Errorf("mirror.Rev = %q, want it to resolve to nixpkgs (%q)", mir.Rev, np.Rev)
	}

	// hmc has follows sub-edges but is itself a real, updatable input.
	hmc := byName["hmc"]
	if hmc.Follows {
		t.Errorf("hmc should not be a follows alias")
	}
	if hmc.Pin() != "abcabcabcabcabcabcabcabcabcabcabcabcabca" {
		t.Errorf("hmc.Pin() = %q", hmc.Pin())
	}
}

func TestParseLockFileMatchesMetadata(t *testing.T) {
	// A raw flake.lock is exactly metadata's `.locks` sub-object, so parsing one
	// (as Upstream does with --output-lock-file) must yield the same inputs as
	// reading it through `nix flake metadata`.
	md := loadFixture(t)
	raw, err := json.Marshal(md.Locks) // {root, nodes} — the on-disk lock shape
	if err != nil {
		t.Fatalf("marshal locks: %v", err)
	}
	got, err := parseLockFile(raw)
	if err != nil {
		t.Fatalf("parseLockFile: %v", err)
	}
	a, err := got.buildInputs()
	if err != nil {
		t.Fatalf("buildInputs(parsed): %v", err)
	}
	b, err := md.buildInputs()
	if err != nil {
		t.Fatalf("buildInputs(fixture): %v", err)
	}
	if len(a) != len(b) {
		t.Fatalf("parsed lock yielded %d inputs, metadata yielded %d", len(a), len(b))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Errorf("input %d differs:\n parsed:   %+v\n metadata: %+v", i, a[i], b[i])
		}
	}
}

func TestParseLockFileMalformed(t *testing.T) {
	if _, err := parseLockFile([]byte("{not json")); err == nil {
		t.Error("parseLockFile should reject malformed JSON")
	}
}

// TestParseLockFileRealLock parses a genuine on-disk flake.lock (the shape
// `nix flake update --output-lock-file` writes, which Upstream reads back): a
// top-level `version` sibling and node-level `original`/`flake` fields the
// metadata struct does not model. They must be tolerated, and the graph must
// resolve — including a root-level `follows` edge.
func TestParseLockFileRealLock(t *testing.T) {
	data, err := os.ReadFile("testdata/flake.lock")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	md, err := parseLockFile(data)
	if err != nil {
		t.Fatalf("parseLockFile (real lock with version/original/flake fields): %v", err)
	}
	inputs, err := md.buildInputs()
	if err != nil {
		t.Fatalf("buildInputs: %v", err)
	}
	byName := map[string]Input{}
	for _, in := range inputs {
		byName[in.Name] = in
	}

	np := byName["nixpkgs"]
	if np.Type != "github" || np.Rev != "8f0500b9660505dc3cb647775fe9a978a74b5283" {
		t.Errorf("nixpkgs parsed wrong: %+v", np)
	}
	if np.URL != "github:nixos/nixpkgs" {
		t.Errorf("nixpkgs.URL = %q, want synthesized github:nixos/nixpkgs", np.URL)
	}

	tot := byName["totality"]
	if tot.Type != "git" || tot.Ref != "main" || tot.LastModified != 1784153891 {
		t.Errorf("totality parsed wrong: %+v", tot)
	}

	mir := byName["mirror"]
	if !mir.Follows {
		t.Error("mirror should be a follows alias")
	}
	if mir.Rev != np.Rev {
		t.Errorf("mirror should resolve to nixpkgs' rev, got %q", mir.Rev)
	}
}

func TestNodeKeyFollows(t *testing.T) {
	md := loadFixture(t)
	// Direct string edge.
	if key, err := md.nodeKey("nixpkgs"); err != nil || key != "nixpkgs" {
		t.Errorf("nodeKey(nixpkgs) = %q, %v; want nixpkgs, nil", key, err)
	}
	// Follows edge walks back to the root's nixpkgs.
	if key, err := md.nodeKey("mirror"); err != nil || key != "nixpkgs" {
		t.Errorf("nodeKey(mirror) = %q, %v; want nixpkgs, nil", key, err)
	}
	// Unknown input.
	if _, err := md.nodeKey("nope"); err == nil {
		t.Errorf("nodeKey(nope) should error")
	}
}

func TestPinFallsBackToNarHash(t *testing.T) {
	in := Input{Rev: "", NarHash: "sha256-XYZ="}
	if in.Pin() != "sha256-XYZ=" {
		t.Errorf("Pin() = %q, want the narHash when Rev is empty", in.Pin())
	}
	in.Rev = "deadbeef"
	if in.Pin() != "deadbeef" {
		t.Errorf("Pin() = %q, want the rev when present", in.Pin())
	}
}

func TestDisplayURL(t *testing.T) {
	cases := []struct {
		l    lockedSource
		want string
	}{
		{lockedSource{Type: "git", URL: "ssh://x/y.git"}, "ssh://x/y.git"},
		{lockedSource{Type: "github", Owner: "nixos", Repo: "nixpkgs"}, "github:nixos/nixpkgs"},
		{lockedSource{Type: "path"}, ""},
	}
	for _, c := range cases {
		if got := c.l.displayURL(); got != c.want {
			t.Errorf("displayURL(%+v) = %q, want %q", c.l, got, c.want)
		}
	}
}

func TestUnknownInputError(t *testing.T) {
	err := &UnknownInputError{Name: "nope", Available: []string{"a", "b"}}
	if err.Error() == "" {
		t.Error("UnknownInputError.Error() empty")
	}
}
