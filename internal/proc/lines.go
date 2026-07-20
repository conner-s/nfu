package proc

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
)

// RunLines runs name with args in dir, streaming its combined stdout+stderr to
// onLine one line at a time. It is the captured counterpart to Stream for the
// manager TUI: bubbletea owns the terminal there, so the child cannot inherit
// stdio — its output is piped into the run view's log pane instead. Stdin is
// /dev/null; anything that must prompt (a sudo password) has to be settled
// before the run starts (see the manager's `sudo -v` pre-auth).
//
// Lines are sanitized for that pane: carriage-return progress redraws (nh/nom
// spinners) split like newlines, ANSI escape sequences and control characters
// are stripped, and runs of blank lines collapse. The child is also asked to
// keep it plain via TERM=dumb/NO_COLOR — the stripping is the backstop for
// tools that colorize pipes anyway.
//
// onLine is called from the calling goroutine, in order, with the terminator
// stripped. A non-zero exit is returned as a CmdError, like Stream.
func RunLines(ctx context.Context, dir string, onLine func(string), name string, args ...string) error {
	bin, err := exec.LookPath(name)
	if err != nil {
		return fmt.Errorf("required program %q not found in PATH", name)
	}
	pr, pw, err := os.Pipe()
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = dir
	cmd.Stdout = pw
	cmd.Stderr = pw // one pipe keeps the child's own interleaving
	cmd.Env = append(os.Environ(), "TERM=dumb", "NO_COLOR=1", "CLICOLOR=0")
	if err := cmd.Start(); err != nil {
		pr.Close()
		pw.Close()
		return &CmdError{Name: name, Args: args, Err: err}
	}
	pw.Close() // the child holds the write end now; EOF arrives when it exits

	sc := bufio.NewScanner(pr)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	sc.Split(scanTerminalLines)
	lastBlank := true // also swallows leading blanks
	for sc.Scan() {
		l := stripControl(sc.Text())
		blank := strings.TrimSpace(l) == ""
		if blank && lastBlank {
			continue
		}
		lastBlank = blank
		onLine(l)
	}
	pr.Close()
	if err := cmd.Wait(); err != nil {
		return &CmdError{Name: name, Args: args, Err: err}
	}
	return sc.Err()
}

// scanTerminalLines is a bufio.SplitFunc that ends a line on \n or \r, so a
// progress bar redrawing itself with bare carriage returns arrives as separate
// lines instead of one giant token. \r\n counts once; a \r at the end of the
// buffer is emitted immediately (a following \n then yields a blank line,
// which the caller's blank-collapse drops).
func scanTerminalLines(data []byte, atEOF bool) (int, []byte, error) {
	if atEOF && len(data) == 0 {
		return 0, nil, nil
	}
	if i := bytes.IndexAny(data, "\r\n"); i >= 0 {
		adv := i + 1
		if data[i] == '\r' && adv < len(data) && data[adv] == '\n' {
			adv++
		}
		return adv, data[:i], nil
	}
	if atEOF {
		return len(data), data, nil
	}
	return 0, nil, nil
}

// ansiRE matches ANSI escape sequences: CSI (colors, cursor movement), OSC
// (titles, hyperlinks) with BEL or ST terminators, and lone two-byte escapes.
var ansiRE = regexp.MustCompile(`\x1b(?:\[[0-9;:?<=>]*[ -/]*[@-~]|\][^\x07\x1b]*(?:\x07|\x1b\\)?|[@-Z\\-_])`)

// stripControl removes ANSI escape sequences and non-printing control bytes so
// a captured line can be rendered inside a TUI frame without the terminal
// executing them. Tabs survive as-is.
func stripControl(s string) string {
	if strings.ContainsRune(s, '\x1b') {
		s = ansiRE.ReplaceAllString(s, "")
	}
	return strings.Map(func(r rune) rune {
		if r == '\t' {
			return r
		}
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, s)
}
