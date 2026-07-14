// Package flake is nfu's typed interface to the parts of `nix flake …` it
// needs: reading the lock (metadata) and moving individual inputs (update).
// It shells out to a real `nix` — never a shell — and keeps every command
// scoped to one flake directory.
package flake

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"sync"

	"github.com/conner-s/nfu/internal/config"
)

// Client runs nix flake commands against a single flake root.
type Client struct {
	root string
	bins *binCache
}

// New builds a client that operates on cfg's resolved flake root. root must be
// an absolute, validated flake directory (see config.Resolve).
func New(root string) *Client {
	return &Client{root: root, bins: newBinCache()}
}

// NewFromConfig is a convenience for the common path: resolve the root from a
// config + --flake override, then build a client.
func NewFromConfig(cfg *config.Config, override string) (*Client, error) {
	root, err := config.Resolve(cfg, override)
	if err != nil {
		return nil, err
	}
	return New(root), nil
}

// Root returns the flake directory this client operates on.
func (c *Client) Root() string { return c.root }

// nixArgs prefixes the given nix subcommand args with the experimental-features
// flag so nfu works even where flakes are not enabled in nix.conf (CI, minimal
// containers). --extra-experimental-features is additive: it never disables a
// feature the host already has on.
func nixArgs(sub ...string) []string {
	return append([]string{"--extra-experimental-features", "nix-command flakes"}, sub...)
}

// binCache resolves trusted binaries once via exec.LookPath so a tampered PATH
// can't shadow them mid-session. Resolution is lazy and cached.
type binCache struct {
	mu   sync.Mutex
	seen map[string]string
}

func newBinCache() *binCache { return &binCache{seen: map[string]string{}} }

// path returns the absolute path of a binary, resolving and caching on first
// use. A missing binary is a typed, user-facing error.
func (b *binCache) path(name string) (string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if p, ok := b.seen[name]; ok {
		if p == "" {
			return "", fmt.Errorf("required program %q not found in PATH", name)
		}
		return p, nil
	}
	p, err := exec.LookPath(name)
	if err != nil {
		b.seen[name] = ""
		return "", fmt.Errorf("required program %q not found in PATH", name)
	}
	b.seen[name] = p
	return p, nil
}

// cmdResult holds captured output of a finished command.
type cmdResult struct {
	stdout []byte
	stderr []byte
}

// run executes name with args in the flake root, NO shell: exec.CommandContext
// with an arg slice. name is resolved through the bin cache. stdout/stderr are
// captured separately. On non-zero exit the error includes trimmed stderr.
func (c *Client) run(ctx context.Context, name string, args ...string) (cmdResult, error) {
	bin, err := c.bins.path(name)
	if err != nil {
		return cmdResult{}, err
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = c.root
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	res := cmdResult{stdout: stdout.Bytes(), stderr: stderr.Bytes()}
	if err != nil {
		return res, &CmdError{
			Name:   name,
			Args:   args,
			Err:    err,
			Stderr: bytes.TrimSpace(stderr.Bytes()),
		}
	}
	return res, nil
}

// CmdError is a failed external command, carrying its stderr for diagnosis.
type CmdError struct {
	Name   string
	Args   []string
	Err    error
	Stderr []byte
}

func (e *CmdError) Error() string {
	if len(e.Stderr) > 0 {
		return fmt.Sprintf("%s failed: %v: %s", e.Name, e.Err, e.Stderr)
	}
	return fmt.Sprintf("%s failed: %v", e.Name, e.Err)
}

func (e *CmdError) Unwrap() error { return e.Err }
