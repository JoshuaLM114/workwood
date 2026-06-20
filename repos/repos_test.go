package repos

import (
	"os"
	"path/filepath"
	"testing"
)

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

func mustMkdir(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
}
