package superfeature

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JoshuaLM114/workwood/config"
	"github.com/JoshuaLM114/workwood/manifest"
	"github.com/JoshuaLM114/workwood/projectdef"
)

// TestCreateUsesShorthandForBranch proves the shorthand becomes the worktree
// branch prefix: creating "my-new-super-feature" defaults the shorthand to "mnsf",
// and an added worktree gets a branch like "mnsf/api".
func TestCreateUsesShorthandForBranch(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	root := t.TempDir()

	// A real base clone (CheckReposReady requires it) on main.
	mainDir := filepath.Join(root, "main")
	mk(t, filepath.Join(mainDir, "svc"))
	git(t, filepath.Join(mainDir, "svc"), "init", "-q", "-b", "main")
	git(t, filepath.Join(mainDir, "svc"), "commit", "-q", "--allow-empty", "-m", "init")

	repoRoot := filepath.Join(root, "super")
	mk(t, repoRoot)
	pdFile := filepath.Join(repoRoot, "workwood.yml")
	if err := os.WriteFile(pdFile, []byte("name: demo\norg: o\nrepos:\n  - name: svc\n    default_branch: main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		Root:         repoRoot,
		ProjectDef:   pdFile,
		MainDir:      mainDir,
		FeaturesDir:  filepath.Join(root, "features"),
		ManifestsDir: filepath.Join(root, "manifests"),
		StateFile:    filepath.Join(root, "state.yml"),
	}

	if err := Create(cfg, "my-new-super-feature", "", "demo"); err != nil {
		t.Fatalf("Create: %v", err)
	}
	m, err := manifest.Load(cfg.ManifestPath("my-new-super-feature"))
	if err != nil {
		t.Fatal(err)
	}
	if m.Shorthand != "mnsf" {
		t.Fatalf("shorthand = %q, want mnsf", m.Shorthand)
	}

	pd, err := projectdef.Load(pdFile)
	if err != nil {
		t.Fatal(err)
	}
	wt, err := Add(cfg, pd, "my-new-super-feature", AddSpec{Repo: "svc", Sub: "api"})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if wt.Branch != "mnsf/api" {
		t.Fatalf("branch = %q, want mnsf/api", wt.Branch)
	}
}

// git runs a git command in dir, failing the test on error.
func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, _ := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	return strings.TrimSpace(string(out))
}

// TestUpLinksRemoteBranch is the core guarantee for pulling a teammate's manifest:
// a worktree whose branch already exists on origin must TRACK it; one that doesn't
// must become a fresh local branch. The remote branch is created AFTER the base is
// cloned, so this also proves Up fetches before deciding.
func TestUpLinksRemoteBranch(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	root := t.TempDir()

	// An "upstream" repo (acts as origin) with just main to start.
	up := filepath.Join(root, "upstream")
	mk(t, up)
	git(t, up, "init", "-q", "-b", "main")
	git(t, up, "commit", "-q", "--allow-empty", "-m", "init")

	// The developer clones the base repo (sees only origin/main so far).
	mainDir := filepath.Join(root, "main")
	mk(t, mainDir)
	if out, err := exec.Command("git", "clone", "-q", up, filepath.Join(mainDir, "svc")).CombinedOutput(); err != nil {
		t.Fatalf("clone: %v\n%s", err, out)
	}

	// A teammate pushes a feature branch to origin AFTER the clone.
	git(t, up, "branch", "feat/exists", "main")

	cfg := &config.Config{
		MainDir:      mainDir,
		FeaturesDir:  filepath.Join(root, "features"),
		ManifestsDir: filepath.Join(root, "manifests"),
	}
	m := &manifest.Manifest{ID: "u", Feature: "demo", Worktrees: []manifest.Worktree{
		{Repo: "svc", Branch: "feat/exists", Base: "main", Path: "demo/svc-exists"},
		{Repo: "svc", Branch: "feat/new", Base: "main", Path: "demo/svc-new"},
	}}
	if err := manifest.Save(cfg.ManifestPath("demo"), m); err != nil {
		t.Fatal(err)
	}

	if _, err := Up(cfg, "demo", nil); err != nil {
		t.Fatalf("Up: %v", err)
	}

	// The branch that exists on origin → the worktree tracks origin/feat/exists.
	wtExists := cfg.Abs("demo/svc-exists")
	if got := gitOut(t, wtExists, "rev-parse", "--abbrev-ref", "HEAD"); got != "feat/exists" {
		t.Errorf("svc-exists on branch %q, want feat/exists", got)
	}
	if up := gitOut(t, wtExists, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{u}"); up != "origin/feat/exists" {
		t.Errorf("svc-exists upstream = %q, want origin/feat/exists", up)
	}

	// The branch that doesn't exist on origin → a fresh local branch, no upstream.
	wtNew := cfg.Abs("demo/svc-new")
	if got := gitOut(t, wtNew, "rev-parse", "--abbrev-ref", "HEAD"); got != "feat/new" {
		t.Errorf("svc-new on branch %q, want feat/new", got)
	}
	// @{u} errors (prints to stderr) when there's no upstream — so it must NOT
	// resolve to an origin ref.
	if up := gitOut(t, wtNew, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{u}"); strings.HasPrefix(up, "origin/") {
		t.Errorf("svc-new should have no upstream, got %q", up)
	}
}

// TestUpSkipsNewBranchWhenDeclined verifies the prompt path: when a worktree's
// branch is on neither local nor origin and onNew returns false, the worktree is
// skipped (not created) rather than inventing a local branch.
func TestUpSkipsNewBranchWhenDeclined(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	root := t.TempDir()
	up := filepath.Join(root, "upstream")
	mk(t, up)
	git(t, up, "init", "-q", "-b", "main")
	git(t, up, "commit", "-q", "--allow-empty", "-m", "init")
	mainDir := filepath.Join(root, "main")
	mk(t, mainDir)
	if out, err := exec.Command("git", "clone", "-q", up, filepath.Join(mainDir, "svc")).CombinedOutput(); err != nil {
		t.Fatalf("clone: %v\n%s", err, out)
	}
	cfg := &config.Config{
		MainDir:      mainDir,
		FeaturesDir:  filepath.Join(root, "features"),
		ManifestsDir: filepath.Join(root, "manifests"),
	}
	m := &manifest.Manifest{ID: "u", Feature: "demo", Worktrees: []manifest.Worktree{
		{Repo: "svc", Branch: "feat/new", Base: "main", Path: "demo/svc-new"},
	}}
	if err := manifest.Save(cfg.ManifestPath("demo"), m); err != nil {
		t.Fatal(err)
	}

	asked := false
	if _, err := Up(cfg, "demo", func(repo, branch string) bool { asked = true; return false }); err != nil {
		t.Fatal(err)
	}
	if !asked {
		t.Error("onNew should have been asked for a branch with no remote")
	}
	if _, err := os.Stat(cfg.Abs("demo/svc-new")); !os.IsNotExist(err) {
		t.Error("a declined worktree should not have been created")
	}
}

func mk(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
}
