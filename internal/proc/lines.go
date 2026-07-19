package proc

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
)

// RunLines runs name with args in dir, streaming its combined stdout+stderr to
// onLine one line at a time. It is the captured counterpart to Stream for the
// manager TUI: bubbletea owns the terminal there, so the child cannot inherit
// stdio — its output is piped into the run view's log pane instead. Stdin is
// /dev/null; anything that must prompt (a sudo password) has to be settled
// before the run starts (see the manager's `sudo -v` pre-auth).
//
// onLine is called from the calling goroutine, in order, with the trailing
// newline stripped. A non-zero exit is returned as a CmdError, like Stream.
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
	if err := cmd.Start(); err != nil {
		pr.Close()
		pw.Close()
		return &CmdError{Name: name, Args: args, Err: err}
	}
	pw.Close() // the child holds the write end now; EOF arrives when it exits

	sc := bufio.NewScanner(pr)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		onLine(sc.Text())
	}
	pr.Close()
	if err := cmd.Wait(); err != nil {
		return &CmdError{Name: name, Args: args, Err: err}
	}
	return sc.Err()
}
