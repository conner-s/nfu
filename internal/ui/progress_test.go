package ui

import (
	"bytes"
	"strings"
	"testing"

	"github.com/conner-s/nfu/internal/flake"
)

func TestSpinnerDisabledIsInert(t *testing.T) {
	var buf bytes.Buffer
	sp := NewStyles().StartSpinner(&buf, false, "nixpkgs  updating…")
	sp.Stop()
	sp.Stop() // idempotent
	if buf.Len() != 0 {
		t.Errorf("disabled spinner wrote output: %q", buf.String())
	}
}

func TestSpinnerDrawsAndClears(t *testing.T) {
	var buf bytes.Buffer
	sp := NewStyles().StartSpinner(&buf, true, "nixpkgs  updating…")
	sp.Stop() // joins the goroutine, so reading buf afterwards is race-free
	sp.Stop() // idempotent
	out := buf.String()
	if !strings.Contains(out, "updating…") {
		t.Errorf("spinner never drew its text: %q", out)
	}
	if !strings.HasSuffix(out, "\r\x1b[2K") {
		t.Errorf("Stop should leave the line cleared, got tail %q", out[max(0, len(out)-16):])
	}
}

func TestRenderResultRowAlignsAndFailedRow(t *testing.T) {
	s := NewStyles()
	r := flake.UpdateResult{Input: "npk", OldPin: "same", NewPin: "same", Unchanged: true}
	row := RenderResultRow(s, NewResultRow(r), 8, 1_784_100_000)
	if !strings.Contains(row, "npk     ") { // padded to the batch's widest name
		t.Errorf("row should pad the name to nameWidth:\n%q", row)
	}
	fail := RenderFailedRow(s, Name("totality"), 8)
	if !strings.Contains(fail, GlyphError) || !strings.Contains(fail, "update failed") {
		t.Errorf("failed row missing glyph or label:\n%q", fail)
	}
	upd := RenderUpdatingText(s, Name("npk"), 8)
	if !strings.Contains(upd, "updating…") {
		t.Errorf("updating text missing label:\n%q", upd)
	}
}
