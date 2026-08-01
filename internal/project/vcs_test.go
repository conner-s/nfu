package project

import (
	"context"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/conner-s/nfu/internal/config"
)

// gitRepo makes a temp git repo, or skips the test where git is unavailable
// (the Nix check sandbox has no git, and none of nfu's other tests need one).
func gitRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not in PATH")
	}
	dir := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		dir = resolved // macOS /var → /private/var, so paths compare equal
	}
	for _, args := range [][]string{
		{"init"},
		{"config", "user.email", "nfu@example.test"},
		{"config", "user.name", "nfu test"},
		{"config", "commit.gpgsign", "false"},
	} {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Skipf("git %v: %v: %s", args, err, out)
		}
	}
	return dir
}

func TestPendingFilesOffersOnlyWhatChanged(t *testing.T) {
	ctx := context.Background()
	dir := gitRepo(t)
	projects := filepath.Join(dir, "projects")

	if err := Save(projects, "totality", config.Project{Inputs: []string{"totality"}}); err != nil {
		t.Fatalf("save: %v", err)
	}
	path := FilePath(projects, "totality")

	// The index does not exist yet, so only the project file is offered.
	pend := PendingFiles(ctx, CommitMessage("add", "totality"), path, IndexPath(dir))
	if len(pend) != 1 {
		t.Fatalf("pending = %+v, want one repo", pend)
	}
	if pend[0].Root != dir {
		t.Errorf("root = %q, want %q", pend[0].Root, dir)
	}
	if got := pend[0].Paths; !reflect.DeepEqual(got, []string{"projects/totality.toml"}) {
		t.Errorf("paths = %v", got)
	}
	if got, want := pend[0].Message, "nfu: add project totality"; got != want {
		t.Errorf("message = %q, want %q", got, want)
	}

	if err := pend[0].Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}
	// Committed and unchanged since: nothing left to offer.
	if got := PendingFiles(ctx, "nfu: update project totality", path); len(got) != 0 {
		t.Errorf("clean file still offered: %+v", got)
	}

	// An edit is offered again, and only that file lands in the commit.
	if err := Save(projects, "totality", config.Project{Inputs: []string{"totality", "npk"}}); err != nil {
		t.Fatalf("re-save: %v", err)
	}
	if err := SaveIndex(dir, &Index{Order: []string{"totality"}}); err != nil {
		t.Fatalf("save index: %v", err)
	}
	pend = PendingFiles(ctx, CommitMessage("update", "totality"), path, IndexPath(dir))
	if len(pend) != 1 {
		t.Fatalf("pending after edit = %+v", pend)
	}
	if got := pend[0].Paths; !reflect.DeepEqual(got, []string{"projects.toml", "projects/totality.toml"}) {
		t.Errorf("paths = %v, want both files", got)
	}
	if got, want := pend[0].Label(), "projects.toml, projects/totality.toml → "+filepath.Base(dir); got != want {
		t.Errorf("label = %q, want %q", got, want)
	}
	if err := pend[0].Commit(ctx); err != nil {
		t.Fatalf("commit edit: %v", err)
	}
}

// A project store outside any repository is never offered — the save stands on
// its own and nothing is pushed at the user.
func TestPendingFilesIgnoresUntrackedStore(t *testing.T) {
	dir := t.TempDir()
	if err := Save(dir, "totality", config.Project{Inputs: []string{"totality"}}); err != nil {
		t.Fatalf("save: %v", err)
	}
	if got := PendingFiles(context.Background(), "nfu: add project totality", FilePath(dir, "totality")); len(got) != 0 {
		t.Errorf("pending outside a repo = %+v, want none", got)
	}
}
