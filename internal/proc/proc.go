// Package proc runs the long, interactive child commands the project pipeline
// shells out to — `nh os switch`, `deploy` — with their stdio inherited so
// progress output streams live and a sudo password prompt reaches the terminal.
//
// It is the streaming counterpart to internal/flake's captured runner: there we
// want nix's stderr buffered to parse the pin delta; here we want the child to
// own the terminal. No shell is ever involved; the binary is resolved through
// exec.LookPath and run with an arg slice.
package proc

import (
	"context"
	"fmt"
	"os"
	"os/exec"
)

// Stream runs name with args in dir, inheriting the parent's stdin/stdout/stderr
// so the child streams directly to the terminal. A non-zero exit is returned as
// a CmdError carrying the command for a legible message.
func Stream(ctx context.Context, dir, name string, args ...string) error {
	bin, err := exec.LookPath(name)
	if err != nil {
		return fmt.Errorf("required program %q not found in PATH", name)
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = dir
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return &CmdError{Name: name, Args: args, Err: err}
	}
	return nil
}

// CmdError is a failed streamed command. Its output already went to the terminal,
// so it carries only the command identity and the exit error.
type CmdError struct {
	Name string
	Args []string
	Err  error
}

func (e *CmdError) Error() string { return fmt.Sprintf("%s failed: %v", e.Name, e.Err) }

func (e *CmdError) Unwrap() error { return e.Err }
