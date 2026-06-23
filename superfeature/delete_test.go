package superfeature

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/JoshuaLM114/workwood/config"
	"github.com/JoshuaLM114/workwood/manifest"
	"github.com/JoshuaLM114/workwood/projectdef"
)

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

	cfg := &config.Config{
		MainDir:      mainDir,
		FeaturesDir:  filepath.Join(root, "features"),
		ManifestsDir: filepath.Join(root, "manifests"),
		StateFile:    filepath.Join(root, "state.yml"),
	}
	m := &manifest.Manifest{ID: "d", Feature: "demo", Worktrees: []manifest.Worktree{
		{Repo: "svc", Branch: "demo/a", Base: "main", Path: "demo/svc-a"},
		{Repo: "svc", Branch: "demo/b", Base: "main", Path: "demo/svc-b"},
	}}
	if err := manifest.Save(cfg.ManifestPath("demo"), m); err != nil {
		t.Fatal(err)
	}
	if _, err := Up(cfg, "demo", func(_, _ string) bool { return true }); err != nil {
		t.Fatalf("Up: %v", err)
	}

	aPath, bPath := cfg.Abs("demo/svc-a"), cfg.Abs("demo/svc-b")
	if _, err := os.Stat(aPath); err != nil {
		t.Fatalf("svc-a not built: %v", err)
	}

	plan := []RepoTeardown{
		{Repo: "svc", Branch: "demo/a", Path: "demo/svc-a", RemoveWorktree: true, DeleteBranch: true},
		{Repo: "svc", Branch: "demo/b", Path: "demo/svc-b", RemoveWorktree: false, DeleteFiles: false, DeleteBranch: false},
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

func branchExists(repo, branch string) bool {
	return exec.Command("git", "-C", repo, "rev-parse", "--verify", "refs/heads/"+branch).Run() == nil
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
	cfg := &config.Config{
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
	cfg := &config.Config{
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

// TestAddExistingRemoteBranch is the core of the "from existing" add: adding a
// worktree whose branch already exists on origin (Sub = the raw branch,
// OmitFeaturePrefix) must check it out as a LOCAL branch TRACKING origin — not cut
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
	cfg := &config.Config{
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

	wt, err := Add(cfg, pd, "demo", AddSpec{Repo: "svc", Sub: "feat/exists", OmitFeaturePrefix: true})
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
