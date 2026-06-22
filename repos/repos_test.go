package repos

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/JoshuaLM114/workwood/config"
	"github.com/JoshuaLM114/workwood/projectdef"
)

// gitIn runs a git command in dir, failing the test on error.
func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	c := exec.Command("git", append([]string{"-C", dir}, args...)...)
	c.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestAheadBehindAndOutOfSync(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	root := t.TempDir()

	up := filepath.Join(root, "up")
	mustMkdir(t, up)
	gitIn(t, up, "init", "-q", "-b", "main")
	gitIn(t, up, "commit", "-q", "--allow-empty", "-m", "A")

	mainDir := filepath.Join(root, "main")
	mustMkdir(t, mainDir)
	if out, err := exec.Command("git", "clone", "-q", up, filepath.Join(mainDir, "svc")).CombinedOutput(); err != nil {
		t.Fatalf("clone: %v\n%s", err, out)
	}
	base := filepath.Join(mainDir, "svc")

	// Fresh clone: in sync.
	if si := AheadBehind(base); !si.HasUpstream || si.Ahead != 0 || si.Behind != 0 || si.OutOfSync() {
		t.Fatalf("fresh clone: %+v (OutOfSync=%v)", si, si.OutOfSync())
	}

	// Upstream advances; FetchAll should surface that we're 1 behind.
	gitIn(t, up, "commit", "-q", "--allow-empty", "-m", "B")
	cfg := &config.Config{MainDir: mainDir}
	pd := &projectdef.File{Repos: []projectdef.Repo{{Name: "svc"}}}
	FetchAll(cfg, pd)

	if si := AheadBehind(base); si.Behind != 1 || !si.OutOfSync() {
		t.Fatalf("after upstream commit: %+v, want behind=1 & out of sync", si)
	}
	if oos := OutOfSync(cfg, pd); len(oos) != 1 || oos[0] != "svc" {
		t.Errorf("OutOfSync = %v, want [svc]", oos)
	}
}

func TestClassifyClone(t *testing.T) {
	base := t.TempDir()

	// missing: no directory at all.
	if got := ClassifyClone(filepath.Join(base, "nope")); got != StateMissing {
		t.Errorf("missing: got %d, want StateMissing", got)
	}

	// not-git: a dir with no .git.
	notgit := filepath.Join(base, "plain")
	mustMkdir(t, notgit)
	if got := ClassifyClone(notgit); got != StateNotGit {
		t.Errorf("not-git: got %d, want StateNotGit", got)
	}

	// real clone: .git is a directory.
	clone := filepath.Join(base, "clone")
	mustMkdir(t, filepath.Join(clone, ".git"))
	if got := ClassifyClone(clone); got != StateClone {
		t.Errorf("clone: got %d, want StateClone", got)
	}

	// worktree: .git is a FILE (a gitdir pointer), not a directory.
	wt := filepath.Join(base, "wt")
	mustMkdir(t, wt)
	if err := os.WriteFile(filepath.Join(wt, ".git"), []byte("gitdir: /somewhere/.git/worktrees/x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := ClassifyClone(wt); got != StateWorktree {
		t.Errorf("worktree: got %d, want StateWorktree", got)
	}
}

func TestActiveBranchAndCheckout(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		c := exec.Command("git", append([]string{"-C", dir}, args...)...)
		c.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q", "-b", "main")
	run("commit", "-q", "--allow-empty", "-m", "init")
	run("branch", "feature/x")

	if got := ActiveBranch(dir); got != "main" {
		t.Fatalf("ActiveBranch = %q, want main", got)
	}
	// A non-clone path → "".
	if got := ActiveBranch(t.TempDir()); got != "" {
		t.Errorf("ActiveBranch(non-clone) = %q, want \"\"", got)
	}

	if err := Checkout(dir, "feature/x"); err != nil {
		t.Fatalf("Checkout: %v", err)
	}
	if got := ActiveBranch(dir); got != "feature/x" {
		t.Fatalf("after checkout ActiveBranch = %q, want feature/x", got)
	}
	// Checking out a nonexistent branch must return an error (not panic).
	if err := Checkout(dir, "does/not/exist"); err == nil {
		t.Error("Checkout of a missing branch should error")
	}
}

func mustMkdir(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
}
