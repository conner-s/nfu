package ui

import (
	"fmt"
	"io"
	"time"
)

// spinnerFrames is the braille cycle the plain-output spinner animates through
// (the bubbletea views carry their own bubbles/spinner; this one is for
// line-oriented output where no program owns the terminal).
var spinnerFrames = [...]string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// Spinner animates a transient status line in place until stopped. Obtain one
// via Styles.StartSpinner; the zero value is inert and safe to Stop.
type Spinner struct {
	stop chan struct{}
	done chan struct{}
}

// StartSpinner begins animating "  <glyph> <text>" on w, redrawing the same
// line ~10×/second until Stop. text must be a single line, pre-styled by the
// caller. When enabled is false (stdout is not a TTY), it returns an inert
// spinner and prints nothing, keeping piped output clean line-oriented text.
func (s Styles) StartSpinner(w io.Writer, enabled bool, text string) *Spinner {
	if !enabled {
		return &Spinner{}
	}
	sp := &Spinner{stop: make(chan struct{}), done: make(chan struct{})}
	go func() {
		defer close(sp.done)
		t := time.NewTicker(100 * time.Millisecond)
		defer t.Stop()
		for i := 0; ; i++ {
			// \r + erase-line: redraw in place without stair-stepping when the
			// new frame is shorter than the last.
			fmt.Fprintf(w, "\r\x1b[2K  %s %s", s.Warn.Render(spinnerFrames[i%len(spinnerFrames)]), text)
			select {
			case <-sp.stop:
				fmt.Fprint(w, "\r\x1b[2K")
				return
			case <-t.C:
			}
		}
	}()
	return sp
}

// Stop halts the animation and clears its line, leaving the caret at column 0
// for the replacement line. It is idempotent and safe on an inert Spinner.
func (sp *Spinner) Stop() {
	if sp.stop == nil {
		return
	}
	select {
	case <-sp.stop: // already stopped
	default:
		close(sp.stop)
	}
	<-sp.done
}
