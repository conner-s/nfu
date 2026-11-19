package ui

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/conner-s/termsafe"
)

// ShortPin abbreviates a locked pin: a 40-char git rev becomes its 7-char short
// form; a narHash is trimmed of its algorithm prefix and clipped. The result is
// as raw as the pin was. It goes into the flake.lock commit subject as it is;
// to draw a pin, use Pin, which cleans first.
func ShortPin(pin string) string {
	if pin == "" {
		return "—"
	}
	if isHex(pin) && len(pin) >= 12 {
		return pin[:7]
	}
	pin = strings.TrimPrefix(pin, "sha256-")
	pin = strings.TrimPrefix(pin, "sha512-")
	// Twelve characters, not twelve bytes: half a character is a raw byte.
	if r := []rune(pin); len(r) > 12 {
		return string(r[:12])
	}
	return pin
}

func isHex(s string) bool {
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'f', r >= 'A' && r <= 'F':
		default:
			return false
		}
	}
	return s != ""
}

// RelAge renders unix seconds relative to now ("5 seconds ago", "1 hour ago",
// "5 days ago"), with moment.js-style boundaries so round-ish durations read
// naturally. Sub-minute durations report to the second — an upstream probe can
// surface a revision only moments old, and "3 seconds ago" is more useful there
// than a flattened "just now". A zero timestamp (unknown lastModified) renders
// as "—".
func RelAge(unix, now int64) string {
	if unix <= 0 {
		return "—"
	}
	d := now - unix
	if d < 0 {
		d = 0
	}
	const (
		min  = 60
		hour = 60 * min
		day  = 24 * hour
	)
	switch {
	case d == 0:
		return "just now"
	case d < 45:
		return plural(d, "second")
	case d < 45*min:
		return plural(d/min, "minute")
	case d < 90*min:
		return "1 hour ago"
	case d < 22*hour:
		return plural(d/hour, "hour")
	case d < 36*hour:
		return "1 day ago"
	case d < 25*day:
		return plural(d/day, "day")
	case d < 45*day:
		return "1 month ago"
	case d < 320*day:
		return plural(d/(30*day), "month")
	default:
		return plural(d/(365*day), "year")
	}
}

func plural(n int64, unit string) string {
	if n <= 1 {
		return "1 " + unit + " ago"
	}
	return fmt.Sprintf("%d %ss ago", n, unit)
}

// AbsTime renders unix seconds as a local wall-clock stamp ("2026-07-15 11:37"),
// the absolute companion to RelAge shown alongside a probed upstream revision so
// the freshness reads both ways. A zero timestamp renders as "—".
func AbsTime(unix int64) string {
	if unix <= 0 {
		return "—"
	}
	return time.Unix(unix, 0).Format("2006-01-02 15:04")
}

func padRight(s string, n int) string {
	if w := utf8.RuneCountInString(s); w < n {
		return s + strings.Repeat(" ", n-w)
	}
	return s
}

// TitleLine renders the heading of a listing: a name, then the path it stands
// for, then a blank line.
func TitleLine(s Styles, name, path termsafe.Text) string {
	return fmt.Sprintf("%s  %s\n\n", s.Title.Render(name.String()), s.Dim.Render(path.String()))
}

// noteSuffix renders a row's note (InputRow.Note) after the row, or nothing.
func noteSuffix(s Styles, note termsafe.Text) string {
	if note.IsEmpty() {
		return ""
	}
	return "  " + s.Warn.Render("! "+note.String())
}

// RenderInputList renders the flake's inputs as an aligned table.
func RenderInputList(s Styles, inputs []InputRow, now int64) string {
	var wn, wt, wr, wp int
	refs := make([]string, len(inputs))
	for i, r := range inputs {
		refs[i] = r.Ref.String()
		if r.Follows {
			refs[i] = "follows"
		}
		wn = max(wn, utf8.RuneCountInString(r.Name.String()))
		wt = max(wt, utf8.RuneCountInString(r.Type.String()))
		wr = max(wr, utf8.RuneCountInString(refs[i]))
		wp = max(wp, utf8.RuneCountInString(r.Pin.String()))
	}

	var b strings.Builder
	for i, r := range inputs {
		name := s.Value.Render(padRight(r.Name.String(), wn))
		if r.Follows {
			name = s.Dim.Render(padRight(r.Name.String(), wn))
		}
		line := fmt.Sprintf("  %s  %s  %s  %s  %s",
			name,
			s.Label.Render(padRight(r.Type.String(), wt)),
			s.Dim.Render(padRight(refs[i], wr)),
			s.Value.Render(padRight(r.Pin.String(), wp)),
			s.Dim.Render(RelAge(r.LastModified, now)),
		)
		b.WriteString(strings.TrimRight(line, " ") + noteSuffix(s, r.Note) + "\n")
	}
	return b.String()
}

// RenderResults renders the outcome of an update: one line per input, glyph-led.
func RenderResults(s Styles, results []ResultRow, now int64) string {
	var wn int
	for _, r := range results {
		wn = max(wn, utf8.RuneCountInString(r.Input.String()))
	}
	var b strings.Builder
	for _, r := range results {
		b.WriteString(RenderResultRow(s, r, wn, now))
	}
	return b.String()
}

// RenderResultRow renders one update outcome line, glyph-led and newline-
// terminated; nameWidth aligns the input column across rows (pass the widest
// name of the batch). It is the unit the streaming updater prints as each
// input's lock write lands.
func RenderResultRow(s Styles, r ResultRow, nameWidth int, now int64) string {
	name := padRight(r.Input.String(), nameWidth)
	if r.Unchanged {
		return fmt.Sprintf("  %s %s  %s  %s\n",
			s.Dim.Render(GlyphUnchanged), s.Value.Render(name),
			s.Dim.Render("already current"),
			s.Dim.Render(r.OldPin.String()+"  ("+RelAge(r.OldMod, now)+")"))
	}
	return fmt.Sprintf("  %s %s  %s %s %s  %s\n",
		s.Good.Render(GlyphMoved), s.Value.Render(name),
		s.Value.Render(r.OldPin.String()), s.Dim.Render(GlyphArrow), s.Good.Render(r.NewPin.String()),
		s.Dim.Render("("+RelAge(r.OldMod, now)+" "+GlyphArrow+" "+RelAge(r.NewMod, now)+")"))
}

// RenderUpdatingText renders the transient "name  updating…" label the
// streaming updater animates behind the spinner glyph; nameWidth keeps it
// column-aligned with the result row that will replace it.
func RenderUpdatingText(s Styles, name termsafe.Text, nameWidth int) string {
	return fmt.Sprintf("%s  %s", s.Value.Render(padRight(name.String(), nameWidth)), s.Dim.Render("updating…"))
}

// RenderFailedRow marks the input whose `nix flake update` failed mid-run, so
// the streamed list accounts for every input that was attempted.
func RenderFailedRow(s Styles, name termsafe.Text, nameWidth int) string {
	return fmt.Sprintf("  %s %s  %s\n",
		s.Bad.Render(GlyphError), s.Value.Render(padRight(name.String(), nameWidth)),
		s.Bad.Render("update failed"))
}
