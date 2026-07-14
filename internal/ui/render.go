package ui

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/conner-s/nfu/internal/flake"
)

// ShortPin abbreviates a locked pin for display: a 40-char git rev becomes its
// 7-char short form; a narHash is trimmed of its algorithm prefix and clipped.
func ShortPin(pin string) string {
	if pin == "" {
		return "—"
	}
	if isHex(pin) && len(pin) >= 12 {
		return pin[:7]
	}
	pin = strings.TrimPrefix(pin, "sha256-")
	pin = strings.TrimPrefix(pin, "sha512-")
	if len(pin) > 12 {
		return pin[:12]
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

// RelAge renders unix seconds relative to now ("just now", "1 hour ago",
// "5 days ago"), with moment.js-style boundaries so round-ish durations read
// naturally. A zero timestamp (unknown lastModified) renders as "—".
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
	case d < 45:
		return "just now"
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

func padRight(s string, n int) string {
	if w := utf8.RuneCountInString(s); w < n {
		return s + strings.Repeat(" ", n-w)
	}
	return s
}

// RenderInputList renders the flake's inputs as an aligned table.
func RenderInputList(s Styles, inputs []flake.Input, now int64) string {
	type row struct {
		name, typ, ref, pin, age string
		follows                  bool
	}
	rows := make([]row, 0, len(inputs))
	var wn, wt, wr, wp int
	for _, in := range inputs {
		r := row{
			name:    in.Name,
			typ:     in.Type,
			ref:     in.Ref,
			pin:     ShortPin(in.Pin()),
			age:     RelAge(in.LastModified, now),
			follows: in.Follows,
		}
		if r.follows {
			r.ref = "follows"
		}
		rows = append(rows, r)
		wn = max(wn, utf8.RuneCountInString(r.name))
		wt = max(wt, utf8.RuneCountInString(r.typ))
		wr = max(wr, utf8.RuneCountInString(r.ref))
		wp = max(wp, utf8.RuneCountInString(r.pin))
	}

	var b strings.Builder
	for _, r := range rows {
		name := s.Value.Render(padRight(r.name, wn))
		if r.follows {
			name = s.Dim.Render(padRight(r.name, wn))
		}
		line := fmt.Sprintf("  %s  %s  %s  %s  %s",
			name,
			s.Label.Render(padRight(r.typ, wt)),
			s.Dim.Render(padRight(r.ref, wr)),
			s.Value.Render(padRight(r.pin, wp)),
			s.Dim.Render(r.age),
		)
		b.WriteString(strings.TrimRight(line, " ") + "\n")
	}
	return b.String()
}

// RenderResults renders the outcome of an update: one line per input, glyph-led.
func RenderResults(s Styles, results []flake.UpdateResult, now int64) string {
	var wn int
	for _, r := range results {
		wn = max(wn, utf8.RuneCountInString(r.Input))
	}
	var b strings.Builder
	for _, r := range results {
		name := padRight(r.Input, wn)
		switch {
		case r.Unchanged:
			b.WriteString(fmt.Sprintf("  %s %s  %s  %s\n",
				s.Dim.Render(GlyphUnchanged), s.Value.Render(name),
				s.Dim.Render("already current"),
				s.Dim.Render(ShortPin(r.OldPin)+"  ("+RelAge(r.OldMod, now)+")")))
		default:
			b.WriteString(fmt.Sprintf("  %s %s  %s %s %s  %s\n",
				s.Good.Render(GlyphMoved), s.Value.Render(name),
				s.Value.Render(ShortPin(r.OldPin)), s.Dim.Render(GlyphArrow), s.Good.Render(ShortPin(r.NewPin)),
				s.Dim.Render("("+RelAge(r.OldMod, now)+" "+GlyphArrow+" "+RelAge(r.NewMod, now)+")")))
		}
	}
	return b.String()
}
