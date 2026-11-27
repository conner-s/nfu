package lock

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/conner-s/nfu/internal/flake"
)

// A trailer lands in the commit's trailer block, where `git interpret-trailers`
// and forge UIs read it, while the reported message stays the bare subject.
func TestCommitEndsTheMessageWithTrailers(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not in PATH")
	}
	dir := t.TempDir()
	git := func(args ...string) string {
		out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
		if err != nil {
			t.Skipf("git %v: %v: %s", args, err, out)
		}
		return string(out)
	}
	git("init")
	git("config", "user.email", "nfu@example.test")
	git("config", "user.name", "nfu test")
	git("config", "commit.gpgsign", "false")
	if err := os.WriteFile(filepath.Join(dir, "flake.lock"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	results := []flake.UpdateResult{{Input: "t3code", OldPin: "aaaaaaa", NewPin: "bbbbbbb"}}
	committed, msg, warn := Commit(context.Background(), dir, results, true, "T3-Thread: Deploy buttons (thread-1)")
	if !committed || warn != "" {
		t.Fatalf("Commit = %v, %q; want a commit", committed, warn)
	}
	if strings.Contains(msg, "T3-Thread") {
		t.Errorf("reported message %q carries the trailer", msg)
	}
	got := strings.TrimSpace(git("log", "-1", "--format=%(trailers:key=T3-Thread,valueonly)"))
	if got != "Deploy buttons (thread-1)" {
		t.Errorf("T3-Thread trailer = %q", got)
	}
}
