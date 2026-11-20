package tui

import (
	"errors"
	"strings"
	"testing"

	"charm.land/bubbles/v2/spinner"
	"github.com/conner-s/termsafe"

	"github.com/conner-s/nfu/internal/flake"
	"github.com/conner-s/nfu/internal/ui"
)

// segModel is a bare model with a fixed clock, enough to render upstreamSegment.
// ui.NewStyles resolves stdout's color profile, which under `go test` is NoTTY,
// so the styles are unstyled and substring assertions see the plain glyphs/text.
func segModel() model {
	return model{
		styles:      ui.NewStyles(),
		spinner:     spinner.New(spinner.WithSpinner(spinner.Dot)),
		now:         1_784_170_000,
		checking:    map[string]bool{},
		upstream:    map[string]probed{},
		upstreamErr: map[string]termsafe.Text{},
	}
}

// answer is a probe's result as the model stores it when an upstreamMsg lands.
func answer(in flake.Input) probed {
	return probed{in: in, pin: ui.Pin(in.Pin())}
}

func TestUpstreamSegmentUnprobed(t *testing.T) {
	m := segModel()
	if got := m.upstreamSegment(flake.Input{Name: "nixpkgs", Rev: "aaaaaaa"}); got != "" {
		t.Errorf("unprobed input should render no segment, got %q", got)
	}
}

func TestUpstreamSegmentChecking(t *testing.T) {
	m := segModel()
	m.checking["nixpkgs"] = true
	got := m.upstreamSegment(flake.Input{Name: "nixpkgs", Rev: "aaaaaaa"})
	if !strings.Contains(got, "checking upstream") {
		t.Errorf("checking segment = %q, want it to mention checking upstream", got)
	}
}

func TestUpstreamSegmentError(t *testing.T) {
	m := segModel()
	m.upstreamErr["nixpkgs"] = ui.ErrLine(errors.New("boom: no route to host"))
	got := m.upstreamSegment(flake.Input{Name: "nixpkgs", Rev: "aaaaaaa"})
	if !strings.Contains(got, "! boom: no route to host") {
		t.Errorf("error segment = %q, want the error text with a ! marker", got)
	}
}

func TestUpstreamSegmentUpToDate(t *testing.T) {
	m := segModel()
	// A narHash-pinned input (no Rev): up-to-date must be decided by Pin()
	// equality, not Rev, so this guards that the comparison uses Pin().
	in := flake.Input{Name: "flake-utils", NarHash: "sha256-ABC="}
	m.upstream["flake-utils"] = answer(flake.Input{Name: "flake-utils", NarHash: "sha256-ABC="})
	got := m.upstreamSegment(in)
	if !strings.Contains(got, "up to date") || !strings.Contains(got, ui.GlyphUnchanged) {
		t.Errorf("up-to-date segment = %q, want the unchanged glyph + 'up to date'", got)
	}
	if strings.Contains(got, ui.GlyphArrow) {
		t.Errorf("up-to-date segment must not show a move arrow: %q", got)
	}
}

func TestUpstreamSegmentMoved(t *testing.T) {
	m := segModel()
	in := flake.Input{Name: "nixpkgs", Rev: "1111111111111111111111111111111111111111", LastModified: m.now - 3*24*3600}
	up := flake.Input{Name: "nixpkgs", Rev: "2222222222222222222222222222222222222222", LastModified: m.now - 90}
	m.upstream["nixpkgs"] = answer(up)
	got := m.upstreamSegment(in)
	if !strings.Contains(got, ui.GlyphArrow) {
		t.Errorf("moved segment should show the arrow: %q", got)
	}
	if !strings.Contains(got, ui.ShortPin(up.Rev)) {
		t.Errorf("moved segment should show the upstream short pin: %q", got)
	}
	if !strings.Contains(got, ui.RelAge(up.LastModified, m.now)) {
		t.Errorf("moved segment should include the relative age: %q", got)
	}
	if !strings.Contains(got, ui.AbsTime(up.LastModified)) {
		t.Errorf("moved segment should include the absolute timestamp: %q", got)
	}
	if strings.Contains(got, "up to date") {
		t.Errorf("moved segment must not say up to date: %q", got)
	}
}
