package proc

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"

	"forgejo.kube.internal.conn3r.xyz/connerlab/termsafe"
)

// RunLines runs name with args in dir, streaming its combined stdout+stderr to
// onLine one line at a time. It is the captured counterpart to Stream for the
// manager TUI: bubbletea owns the terminal there, so the child cannot inherit
// stdio — its output is piped into the run view's log pane instead. Stdin is
// /dev/null; anything that must prompt (a sudo password) has to be settled
// before the run starts (see the manager's `sudo -v` pre-auth).
//
// Lines are cleaned for that pane, which is why onLine takes a termsafe.Text
// and cannot be handed the raw line: carriage-return progress redraws (nh/nom
// spinners) split like newlines, every control sequence goes with its payload,
// control characters, bidirectional controls and invisible characters are
// removed, a tab becomes a space, and runs of blank lines collapse. This is a
// log that is watched going by, so the policy is termsafe.Drop: nothing marks
// what was removed. The report handed to onLine counts it, and its
// Unterminated field says when a control string without a terminator took the
// rest of a line with it. The child is also asked to keep it plain via
// TERM=dumb/NO_COLOR; the cleaning is what holds when it does not.
//
// onLine is called from the calling goroutine, in order, with the terminator
// stripped. A non-zero exit is returned as a CmdError, like Stream.
func RunLines(ctx context.Context, dir string, onLine func(termsafe.Text, termsafe.Report), name string, args ...string) error {
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
		l, rep := termsafe.Drop.LineReport(sc.Text())
		blank := l.TrimSpace().IsEmpty()
		if blank && lastBlank && rep.Unterminated == 0 {
			continue
		}
		lastBlank = blank
		onLine(l, rep)
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
