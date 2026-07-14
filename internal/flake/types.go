package flake

// Input is one flake input as seen through the lock: its name in flake.nix, the
// source it resolves to, and where it is currently pinned.
type Input struct {
	Name         string // input name as written in flake.nix inputs
	Type         string // locked source type: "github", "git", "path", "tarball", …
	URL          string // locked source URL (best-effort; empty for some types)
	Ref          string // git ref / branch, when the source has one
	Rev          string // locked revision (commit); empty for revless sources
	NarHash      string // content hash; the pin when there is no Rev
	LastModified int64  // unix seconds of the locked source; 0 if unknown
	Follows      bool   // this root input is a `follows` alias, not its own pin
}

// Pin is the short identity shown to the user: the rev when present, else the
// narHash. It is what actually changes across an update.
func (in Input) Pin() string {
	if in.Rev != "" {
		return in.Rev
	}
	return in.NarHash
}

// UpdateResult is the outcome of moving one input. A failure is not per-input:
// `nix flake update` either succeeds or aborts atomically, so an error is
// reported once at the run level (see Client.Update's returned error), not here.
type UpdateResult struct {
	Input      string
	OldPin     string // rev (or narHash) before the update
	NewPin     string // rev (or narHash) after the update
	OldMod     int64  // lastModified before (unix seconds)
	NewMod     int64  // lastModified after (unix seconds)
	Unchanged  bool   // the lock did not move (already current)
	Transition string // nix's "• Updated input …" line(s), verbatim
}

// Moved reports whether the input's pin actually changed.
func (r UpdateResult) Moved() bool { return !r.Unchanged && r.OldPin != r.NewPin }
