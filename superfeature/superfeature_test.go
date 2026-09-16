package superfeature

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JoshuaLM114/workwood/manifest"
	"github.com/JoshuaLM114/workwood/models"
	"github.com/JoshuaLM114/workwood/projectdef"
)

// --- shared helpers ---------------------------------------------------------

// mk makes a directory tree, failing the test on error.
func mk(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
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

// gitOut runs a git command in dir and returns its trimmed combined output.
func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, _ := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	return strings.TrimSpace(string(out))
}

// branchExists reports whether refs/heads/<branch> exists in repo.
func branchExists(repo, branch string) bool {
	return exec.Command("git", "-C", repo, "rev-parse", "--verify", "refs/heads/"+branch).Run() == nil
}

// --- pure helpers (table-driven) -------------------------------------------

func TestResolveBranchWith(t *testing.T) {
	cases := []struct {
		name    string
		feature string
		sub     string
		omit    bool
		want    string
	}{
		{"simple", "demo", "api", false, "demo/api"},
		{"already-prefixed", "demo", "demo/api", false, "demo/api"},
		{"slashed-sub", "demo", "fix/login", false, "demo/fix/login"},
		{"omit-prefix", "demo", "hotfix", true, "hotfix"},
		{"sub-equals-feature", "demo", "demo", false, "demo"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ResolveBranchWith(c.feature, c.sub, c.omit); got != c.want {
				t.Fatalf("ResolveBranchWith(%q,%q,%v) = %q, want %q", c.feature, c.sub, c.omit, got, c.want)
			}
		})
	}
}

func TestValidName(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		wantErr bool
	}{
		{"empty", "", true},
		{"dot", ".", true},
		{"dotdot", "..", true},
		{"slash", "a/b", true},
		{"backslash", `a\b`, true},
		{"leading-dash", "-x", true},
		{"traversal", "../x", true},
		{"trailing-dotdot", "x/..", true},
		{"absolute", "/abs", true},
		{"deep-traversal", "../../etc", true},
		{"plain", "feature", false},
		{"hyphenated", "my-feature", false},
		{"underscore", "repo_1", false},
		{"dotted", "a.b", false},
		{"versioned", "v2", false},
		{"sample-db", "sample-db", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := validName(c.in)
			if c.wantErr && err == nil {
				t.Errorf("validName(%q) = nil, want error", c.in)
			}
			if !c.wantErr && err != nil {
				t.Errorf("validName(%q) = %v, want nil", c.in, err)
			}
		})
	}
}

func TestUnderData(t *testing.T) {
	cfg := &models.Config{FeaturesDir: "/data/features"}
	cases := []struct {
		name string
		path string
		want bool
	}{
		{"nested", "/data/features/feat/repo", true},
		{"direct-child", "/data/features/x", true},
		// "." (the features dir itself), parents, escapes, and sibling prefixes.
		{"features-dir-itself", "/data/features", false},
		{"parent", "/data", false},
		{"escape", "/data/features/../../etc", false},
		{"unrelated", "/etc/passwd", false},
		{"sibling-prefix", "/data/featuresX", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := underData(cfg, c.path); got != c.want {
				t.Errorf("underData(%q) = %v, want %v", c.path, got, c.want)
			}
		})
	}
}

// --- safety -----------------------------------------------------------------

func TestSafeRemoveAll(t *testing.T) {
	root := t.TempDir()
	cfg := &models.Config{FeaturesDir: filepath.Join(root, "features")}

	inside := filepath.Join(cfg.FeaturesDir, "feat")
	if err := os.MkdirAll(inside, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := safeRemoveAll(cfg, inside); err != nil {
		t.Fatalf("safeRemoveAll(inside) = %v, want nil", err)
	}
	if _, err := os.Stat(inside); !os.IsNotExist(err) {
		t.Fatal("inside path should have been removed")
	}

	// A path OUTSIDE the features tree must be refused AND left on disk.
	outside := filepath.Join(root, "outside")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := safeRemoveAll(cfg, outside); err == nil {
		t.Fatal("safeRemoveAll(outside) = nil, want error")
	}
	if _, err := os.Stat(outside); err != nil {
		t.Fatalf("outside path must NOT be deleted, but: %v", err)
	}
}

// The name guard must reject traversal at the public entry points, before any
// path is built from the name.
func TestMutatorsRejectBadNames(t *testing.T) {
	cfg := new(models.Config) // validName runs first; cfg is never touched
	if err := Create(cfg, "../evil", "", ""); err == nil {
		t.Error("Create(../evil) = nil, want error")
	}
	if _, err := Down(cfg, "../evil"); err == nil {
		t.Error("Down(../evil) = nil, want error")
	}
	if err := Delete(cfg, "a/b", false); err == nil {
		t.Error("Delete(a/b) = nil, want error")
	}
	if _, err := DeleteWalk(cfg, "..", nil); err == nil {
		t.Error("DeleteWalk(..) = nil, want error")
	}
}

func TestReconcileProducesOneOutcomePerItem(t *testing.T) {
	cfg := &models.Config{ManifestsDir: t.TempDir(), FeaturesDir: t.TempDir()}
	// No manifest on disk, so each op fails — but Reconcile must continue and
	// return exactly one outcome per planned item, never aborting on the first.
	plan := ReconcilePlan{
		DropMissing: []models.Worktree{{Repo: "a", Branch: "x"}, {Repo: "b", Branch: "y"}},
	}
	if got := Reconcile(cfg, "feat", plan); len(got) != 2 {
		t.Fatalf("Reconcile returned %d outcomes, want 2", len(got))
	}
	if got := Reconcile(cfg, "feat", ReconcilePlan{}); len(got) != 0 {
		t.Errorf("empty plan → %d outcomes, want 0", len(got))
	}
}

// --- create / add -----------------------------------------------------------

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
	if err := os.WriteFile(pdFile, []byte("name: demo\nrepos:\n  - name: svc\n    default_branch: main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := &models.Config{
		Root:         repoRoot,
		ProjectDef:   pdFile,
		MainDir:      mainDir,
		FeaturesDir:  filepath.Join(root, "features"),
		ManifestsDir: filepath.Join(root, "manifests"),
		StateDir:     filepath.Join(root, "state"),
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

// TestAddExistingRemoteBranch is the core of the "from existing" add: adding a
// worktree whose branch already exists on origin (ExistingBranch with the raw
// Sub) must check it out as a LOCAL branch TRACKING origin — not cut
// a new <feature>/<sub> branch.
func TestAddExistingRemoteBranch(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	root := t.TempDir()
	up := filepath.Join(root, "upstream")
	mk(t, up)
	git(t, up, "init", "-q", "-b", "main")
	git(t, up, "commit", "-q", "--allow-empty", "-m", "init")
	git(t, up, "branch", "feat/exists", "main") // a teammate's pushed branch

	mainDir := filepath.Join(root, "main")
	mk(t, mainDir)
	if out, err := exec.Command("git", "clone", "-q", up, filepath.Join(mainDir, "svc")).CombinedOutput(); err != nil {
		t.Fatalf("clone: %v\n%s", err, out)
	}

	repoRoot := filepath.Join(root, "super")
	mk(t, repoRoot)
	pdFile := filepath.Join(repoRoot, "workwood.yml")
	if err := os.WriteFile(pdFile, []byte("name: demo\nrepos:\n  - name: svc\n    default_branch: main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := &models.Config{
		Root: repoRoot, ProjectDef: pdFile, MainDir: mainDir,
		FeaturesDir:  filepath.Join(root, "features"),
		ManifestsDir: filepath.Join(root, "manifests"),
		StateDir:     filepath.Join(root, "state"),
		StateFile:    filepath.Join(root, "state.yml"),
	}
	if err := Create(cfg, "demo", "", "demo"); err != nil {
		t.Fatalf("Create: %v", err)
	}
	pd, err := projectdef.Load(pdFile)
	if err != nil {
		t.Fatal(err)
	}

	wt, err := Add(cfg, pd, "demo", AddSpec{Repo: "svc", Sub: "feat/exists", ExistingBranch: true})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if wt.Branch != "feat/exists" {
		t.Fatalf("branch = %q, want feat/exists (no feature prefix)", wt.Branch)
	}
	abs := cfg.Abs(wt.Path)
	if got := gitOut(t, abs, "rev-parse", "--abbrev-ref", "HEAD"); got != "feat/exists" {
		t.Errorf("HEAD = %q, want feat/exists", got)
	}
	if up := gitOut(t, abs, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{u}"); up != "origin/feat/exists" {
		t.Errorf("upstream = %q, want origin/feat/exists (worktree must track the remote)", up)
	}
}

// --- up ---------------------------------------------------------------------

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

	cfg := &models.Config{
		MainDir:      mainDir,
		FeaturesDir:  filepath.Join(root, "features"),
		ManifestsDir: filepath.Join(root, "manifests"),
	}
	m := &models.Manifest{ID: "u", Feature: "demo", Worktrees: []models.Worktree{
		{Repo: "svc", Branch: "feat/exists", Base: "main", Path: "demo/svc--feat_exists"},
		{Repo: "svc", Branch: "feat/new", Base: "main", Path: "demo/svc--feat_new"},
	}}
	if err := manifest.Save(cfg.ManifestPath("demo"), m); err != nil {
		t.Fatal(err)
	}

	if _, err := Up(cfg, "demo", nil); err != nil {
		t.Fatalf("Up: %v", err)
	}

	// The branch that exists on origin → the worktree tracks origin/feat/exists.
	wtExists := cfg.Abs("demo/svc--feat_exists")
	if got := gitOut(t, wtExists, "rev-parse", "--abbrev-ref", "HEAD"); got != "feat/exists" {
		t.Errorf("svc-exists on branch %q, want feat/exists", got)
	}
	if up := gitOut(t, wtExists, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{u}"); up != "origin/feat/exists" {
		t.Errorf("svc-exists upstream = %q, want origin/feat/exists", up)
	}

	// The branch that doesn't exist on origin → a fresh local branch, no upstream.
	wtNew := cfg.Abs("demo/svc--feat_new")
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
	cfg := &models.Config{
		MainDir:      mainDir,
		FeaturesDir:  filepath.Join(root, "features"),
		ManifestsDir: filepath.Join(root, "manifests"),
	}
	m := &models.Manifest{ID: "u", Feature: "demo", Worktrees: []models.Worktree{
		{Repo: "svc", Branch: "feat/new", Base: "main", Path: "demo/svc--feat_new"},
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
	if _, err := os.Stat(cfg.Abs("demo/svc--feat_new")); !os.IsNotExist(err) {
		t.Error("a declined worktree should not have been created")
	}
}

// --- delete / reconcile -----------------------------------------------------

// TestDeleteWalk builds a two-worktree feature, then deletes it with a mixed plan:
// repo `a` removed worktree + branch; repo `b` kept entirely. Asserts the per-repo
// choices are honoured and the feature record is removed.
func TestDeleteWalk(t *testing.T) {
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

	cfg := &models.Config{
		MainDir:      mainDir,
		FeaturesDir:  filepath.Join(root, "features"),
		ManifestsDir: filepath.Join(root, "manifests"),
		StateFile:    filepath.Join(root, "state.yml"),
	}
	m := &models.Manifest{ID: "d", Feature: "demo", Worktrees: []models.Worktree{
		{Repo: "svc", Branch: "demo/a", Base: "main", Path: "demo/svc--a"},
		{Repo: "svc", Branch: "demo/b", Base: "main", Path: "demo/svc--b"},
	}}
	if err := manifest.Save(cfg.ManifestPath("demo"), m); err != nil {
		t.Fatal(err)
	}
	if _, err := Up(cfg, "demo", func(_, _ string) bool { return true }); err != nil {
		t.Fatalf("Up: %v", err)
	}

	aPath, bPath := cfg.Abs("demo/svc--a"), cfg.Abs("demo/svc--b")
	if _, err := os.Stat(aPath); err != nil {
		t.Fatalf("svc-a not built: %v", err)
	}

	plan := []RepoTeardown{
		{Repo: "svc", Branch: "demo/a", Path: "demo/svc--a", RemoveWorktree: true, DeleteBranch: true},
		{Repo: "svc", Branch: "demo/b", Path: "demo/svc--b", RemoveWorktree: false, DeleteFiles: false, DeleteBranch: false},
	}
	if _, err := DeleteWalk(cfg, "demo", plan); err != nil {
		t.Fatalf("DeleteWalk: %v", err)
	}

	base := cfg.BaseRepo("svc")
	// a: worktree + branch gone.
	if _, err := os.Stat(aPath); !os.IsNotExist(err) {
		t.Error("svc-a worktree should be removed")
	}
	if branchExists(base, "demo/a") {
		t.Error("branch demo/a should be deleted")
	}
	// b: worktree + branch kept.
	if _, err := os.Stat(bPath); err != nil {
		t.Error("svc-b worktree should be kept")
	}
	if !branchExists(base, "demo/b") {
		t.Error("branch demo/b should be kept")
	}
	// The feature record is gone.
	if _, err := os.Stat(cfg.ManifestPath("demo")); !os.IsNotExist(err) {
		t.Error("manifest should be removed")
	}
}

// TestDiagnoseAdoptMissing covers the reconcile core: an on-disk worktree dropped
// from the manifest is detected as an ORPHAN and can be ADOPTED back; a manifest
// entry whose checkout is deleted is detected as MISSING.
func TestDiagnoseAdoptMissing(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	root := t.TempDir()
	mainDir := filepath.Join(root, "main")
	mk(t, filepath.Join(mainDir, "svc"))
	git(t, filepath.Join(mainDir, "svc"), "init", "-q", "-b", "main")
	git(t, filepath.Join(mainDir, "svc"), "commit", "-q", "--allow-empty", "-m", "init")

	repoRoot := filepath.Join(root, "super")
	mk(t, repoRoot)
	pdFile := filepath.Join(repoRoot, "workwood.yml")
	if err := os.WriteFile(pdFile, []byte("name: demo\nrepos:\n  - name: svc\n    default_branch: main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := &models.Config{
		Root: repoRoot, ProjectDef: pdFile, MainDir: mainDir,
		FeaturesDir:  filepath.Join(root, "features"),
		ManifestsDir: filepath.Join(root, "manifests"),
		StateDir:     filepath.Join(root, "state"),
		StateFile:    filepath.Join(root, "state.yml"),
	}
	if err := Create(cfg, "demo", "", "demo"); err != nil {
		t.Fatalf("Create: %v", err)
	}
	pd, err := projectdef.Load(pdFile)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyEdit(cfg, pd, "demo", "", []AddSpec{
		{Repo: "svc", Sub: "a"}, {Repo: "svc", Sub: "b"},
	}, nil); err != nil {
		t.Fatalf("ApplyEdit: %v", err)
	}

	// Simulate the desync: drop the d/a entry from the manifest, leaving its
	// worktree on disk (an orphan).
	m, _ := manifest.Load(cfg.ManifestPath("demo"))
	if idx := m.Find("svc", "d/a"); idx >= 0 {
		m.Worktrees = append(m.Worktrees[:idx], m.Worktrees[idx+1:]...)
	}
	if err := manifest.Save(cfg.ManifestPath("demo"), m); err != nil {
		t.Fatal(err)
	}

	d, err := Diagnose(cfg, pd, "demo")
	if err != nil {
		t.Fatalf("Diagnose: %v", err)
	}
	if len(d.Orphans) != 1 || d.Orphans[0].Repo != "svc" || d.Orphans[0].Branch != "d/a" {
		t.Fatalf("orphans = %+v, want one svc d/a", d.Orphans)
	}
	if len(d.Missing) != 0 {
		t.Fatalf("missing = %+v, want none", d.Missing)
	}

	// Adopt it back → the manifest tracks it again, and the feature is in sync.
	if err := AdoptOrphan(cfg, "demo", d.Orphans[0]); err != nil {
		t.Fatalf("AdoptOrphan: %v", err)
	}
	if d2, _ := Diagnose(cfg, pd, "demo"); !d2.OK() {
		t.Errorf("after adopt, want OK, got orphans=%v missing=%v", d2.Orphans, d2.Missing)
	}

	// Now delete a checkout on disk → detected as missing.
	if err := os.RemoveAll(cfg.Abs("demo/svc--b")); err != nil {
		t.Fatal(err)
	}
	d3, _ := Diagnose(cfg, pd, "demo")
	if len(d3.Missing) != 1 || d3.Missing[0].Branch != "d/b" {
		t.Fatalf("missing = %+v, want one d/b", d3.Missing)
	}
}

// TestApplyEditPersistsPartial proves the desync fix: when a batch apply fails on a
// later add, the worktrees already provisioned are SAVED to the manifest (disk and
// manifest stay consistent) rather than lost. The second add (d/a/b) is rejected by
// the ref-hierarchy guard because the first (d/a) now exists.
func TestApplyEditPersistsPartial(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	root := t.TempDir()
	mainDir := filepath.Join(root, "main")
	mk(t, filepath.Join(mainDir, "svc"))
	git(t, filepath.Join(mainDir, "svc"), "init", "-q", "-b", "main")
	git(t, filepath.Join(mainDir, "svc"), "commit", "-q", "--allow-empty", "-m", "init")

	repoRoot := filepath.Join(root, "super")
	mk(t, repoRoot)
	pdFile := filepath.Join(repoRoot, "workwood.yml")
	if err := os.WriteFile(pdFile, []byte("name: demo\nrepos:\n  - name: svc\n    default_branch: main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := &models.Config{
		Root: repoRoot, ProjectDef: pdFile, MainDir: mainDir,
		FeaturesDir:  filepath.Join(root, "features"),
		ManifestsDir: filepath.Join(root, "manifests"),
		StateDir:     filepath.Join(root, "state"),
		StateFile:    filepath.Join(root, "state.yml"),
	}
	if err := Create(cfg, "demo", "", "demo"); err != nil { // shorthand → "d"
		t.Fatalf("Create: %v", err)
	}
	pd, err := projectdef.Load(pdFile)
	if err != nil {
		t.Fatal(err)
	}

	// Two adds; the second nests under the first's branch → ref-hierarchy error.
	_, err = ApplyEdit(cfg, pd, "demo", "", []AddSpec{
		{Repo: "svc", Sub: "a"},
		{Repo: "svc", Sub: "a/b"},
	}, nil)
	if err == nil {
		t.Fatal("expected the second add (d/a/b) to fail the ref-hierarchy guard")
	}

	// The first add must be PERSISTED despite the later failure (no desync).
	m, lerr := manifest.Load(cfg.ManifestPath("demo"))
	if lerr != nil {
		t.Fatal(lerr)
	}
	if len(m.Worktrees) != 1 || m.Worktrees[0].Branch != "d/a" {
		t.Fatalf("manifest = %+v, want exactly the d/a worktree saved", m.Worktrees)
	}
	if _, serr := os.Stat(cfg.Abs(m.Worktrees[0].Path)); serr != nil {
		t.Errorf("the saved worktree should exist on disk: %v", serr)
	}
}
