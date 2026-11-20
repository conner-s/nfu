package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// repo makes a temp git repo with one commit, or skips the test where git is
// unavailable (the Nix check sandbox has none; the dev shell does).
func repo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not in PATH")
	}
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init"},
		{"config", "user.email", "nfu@example.test"},
		{"config", "user.name", "nfu test"},
		{"config", "commit.gpgsign", "false"},
		{"commit", "--allow-empty", "-m", "start"},
	} {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Skipf("git %v: %v: %s", args, err, out)
		}
	}
	return dir
}

func write(t *testing.T, dir, name string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(name+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

// committed lists the files of the last commit.
func committed(t *testing.T, dir string) []string {
	t.Helper()
	out, err := exec.Command("git", "-C", dir, "-c", "core.quotePath=false", "show", "--name-only", "--format=").Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.Fields(strings.TrimSpace(string(out)))
}

// A path nfu gives git is the name of one file. A project file can be called
// anything, and git would read `*.toml` as a pattern and `:(top)…` as
// pathspec magic, even after `--`: the commit would take other files in.
func TestCommitTakesAPathAsAFileNameNotAPattern(t *testing.T) {
	ctx := context.Background()
	for _, odd := range []string{"*.toml", ":(top)a.toml", ":!a.toml", "-b.toml", "--all"} {
		dir := repo(t)
		write(t, dir, "a.toml")
		write(t, dir, "b.toml")
		write(t, dir, odd)

		changed, err := Changed(ctx, dir, odd)
		if err != nil || !changed {
			t.Errorf("Changed(%q) = %v, %v; want true", odd, changed, err)
		}
		if err := Commit(ctx, dir, "nfu: add project", odd); err != nil {
			t.Errorf("Commit(%q): %v", odd, err)
			continue
		}
		if got := committed(t, dir); len(got) != 1 || got[0] != odd {
			t.Errorf("committing %q took %q, want that file alone", odd, got)
		}
	}
}

// A file that is not there is not found through a pattern either.
func TestChangedDoesNotMatchByPattern(t *testing.T) {
	dir := repo(t)
	write(t, dir, "a.toml")
	if changed, err := Changed(context.Background(), dir, "*.toml"); err != nil || changed {
		t.Errorf("Changed(*.toml) = %v, %v with no file of that name; want false", changed, err)
	}
}

// The subject is the value of -m whatever it looks like: lock.Message builds
// it from input names and pins.
func TestCommitSubjectIsNeverAnOption(t *testing.T) {
	ctx := context.Background()
	for _, subject := range []string{"--amend", "-a", "--all", "flake.lock: update --amend (abc → def)"} {
		dir := repo(t)
		write(t, dir, "a.toml")
		write(t, dir, "flake.lock")
		if err := CommitLock(ctx, dir, subject); err != nil {
			t.Errorf("CommitLock(%q): %v", subject, err)
			continue
		}
		out, err := exec.Command("git", "-C", dir, "log", "--format=%s").Output()
		if err != nil {
			t.Fatal(err)
		}
		if subjects := strings.Split(strings.TrimSpace(string(out)), "\n"); len(subjects) != 2 || subjects[0] != subject {
			t.Errorf("after a commit with subject %q the log is %q, want it on top of the first commit", subject, subjects)
		}
		if got := committed(t, dir); len(got) != 1 || got[0] != "flake.lock" {
			t.Errorf("the commit with subject %q took %q, want flake.lock alone", subject, got)
		}
	}
}
