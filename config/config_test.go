package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/JoshuaLM114/workwood/models"
)

func TestFindProject(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ProjectDefName), []byte("id: x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if got, _, _, _ := findProject(sub); got != root {
		t.Fatalf("walk-up: got %q want %q", got, root)
	}
	if got, _, _, _ := findProject(root); got != root {
		t.Fatalf("at-root: got %q want %q", got, root)
	}
	if got, _, _, _ := findProject(t.TempDir()); got != "" {
		t.Fatalf("no project: got %q want \"\"", got)
	}
}

// A feature folder's link.yml resolves to its parent super-repo + active feature,
// and is preferred over a workwood.yml higher up (deepest match wins).
func TestFindProjectViaFeatureLink(t *testing.T) {
	superRepo := t.TempDir()
	if err := os.WriteFile(filepath.Join(superRepo, ProjectDefName), []byte("id: pid\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dataDir := t.TempDir()
	featDir := filepath.Join(dataDir, "features", "demo")
	cfg := &models.Config{Root: superRepo, DataDir: dataDir, ProjectID: "pid", FeaturesDir: filepath.Join(dataDir, "features")}
	if err := WriteFeatureLink(cfg, "demo"); err != nil {
		t.Fatal(err)
	}
	// From a worktree subdir deep inside the feature folder.
	deep := filepath.Join(featDir, "api", "src")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	root, gotData, feature, err := findProject(deep)
	if err != nil {
		t.Fatal(err)
	}
	if root != superRepo || gotData != dataDir || feature != "demo" {
		t.Fatalf("findProject = (%q,%q,%q), want (%q,%q,demo)", root, gotData, feature, superRepo, dataDir)
	}

	// A link pointing at a vanished super-repo is a StaleLinkError.
	cfg.Root = filepath.Join(t.TempDir(), "gone")
	if err := WriteFeatureLink(cfg, "demo"); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := findProject(deep); err == nil {
		t.Error("stale link should error")
	}
}

// LocateProject from deep inside a feature folder resolves the parent super-repo
// + its data dir + the active feature, with no env or cwd in the super-repo.
func TestLocateProjectViaLink(t *testing.T) {
	superRepo := t.TempDir()
	if err := os.WriteFile(filepath.Join(superRepo, ProjectDefName), []byte("id: pid\nname: demo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dataDir := t.TempDir()
	cfg := &models.Config{Root: superRepo, DataDir: dataDir, ProjectID: "pid", FeaturesDir: filepath.Join(dataDir, "features")}
	if err := WriteFeatureLink(cfg, "demo"); err != nil {
		t.Fatal(err)
	}
	deep := filepath.Join(dataDir, "features", "demo", "api")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	loc, err := LocateProject(deep)
	if err != nil {
		t.Fatal(err)
	}
	if loc.Root != superRepo || loc.DataDir != dataDir || loc.ActiveFeature != "demo" {
		t.Fatalf("loc = %+v, want root=%q data=%q feature=demo", loc, superRepo, dataDir)
	}
}

func TestEnsureFeatureLink(t *testing.T) {
	superRepo := t.TempDir()
	if err := os.WriteFile(filepath.Join(superRepo, ProjectDefName), []byte("id: pid\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dataDir := t.TempDir()
	cfg := &models.Config{Root: superRepo, DataDir: dataDir, ProjectID: "pid", FeaturesDir: filepath.Join(dataDir, "features")}

	// Missing → regenerated, then valid.
	if regen, err := EnsureFeatureLink(cfg, "demo"); err != nil || !regen {
		t.Fatalf("missing link should regenerate: regen=%v err=%v", regen, err)
	}
	if !FeatureLinkValid(cfg, "demo") {
		t.Fatal("link should be valid right after writing")
	}
	// Already valid → not rewritten.
	if regen, _ := EnsureFeatureLink(cfg, "demo"); regen {
		t.Error("a valid link should not be rewritten")
	}

	// Inconsistent (super-repo moved) → invalid → regenerated for the new config.
	moved := &models.Config{Root: t.TempDir(), DataDir: dataDir, ProjectID: "pid", FeaturesDir: cfg.FeaturesDir}
	if FeatureLinkValid(moved, "demo") {
		t.Fatal("a link pointing elsewhere should be invalid for the moved config")
	}
	if regen, _ := EnsureFeatureLink(moved, "demo"); !regen {
		t.Error("inconsistent link should regenerate")
	}

	// A future link version is treated as out of date.
	raw := "version: 99\nsuper_repo: " + superRepo + "\ndata_dir: " + dataDir + "\nproject: pid\nfeature: demo\n"
	if err := os.WriteFile(cfg.FeatureLinkPath("demo"), []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	if FeatureLinkValid(cfg, "demo") {
		t.Error("a newer-than-current link version should be invalid (triggers regenerate)")
	}
}

func TestBuildDefaults(t *testing.T) {
	t.Setenv(EnvHome, t.TempDir())
	data := t.TempDir()
	pd := &models.ProjectDef{ID: "pid", Name: "myslug"}

	cfg, err := Build("/some/super-repo", pd, data)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ProjectID != "pid" || cfg.ProjectSlug != "myslug" || cfg.ProjectName != "myslug" {
		t.Fatalf("identity defaults wrong: %+v", cfg)
	}
	if cfg.StateDir != data {
		t.Fatalf("state dir should equal the data dir: %q", cfg.StateDir)
	}
	if cfg.MainDir != filepath.Join(data, "main") || cfg.FeaturesDir != filepath.Join(data, "features") {
		t.Fatalf("default checkout dirs wrong: main=%q features=%q", cfg.MainDir, cfg.FeaturesDir)
	}
	if cfg.ActionsDir != filepath.Join("/some/super-repo", "workwood", "actions") {
		t.Fatalf("actions dir: %q", cfg.ActionsDir)
	}
}

func TestBuildUsesStateName(t *testing.T) {
	t.Setenv(EnvHome, t.TempDir())
	data := t.TempDir()
	pd := &models.ProjectDef{ID: "pid", Name: "myslug"}
	stFile := filepath.Join(data, StateFileName)
	if err := SaveState(stFile, &models.ProjectState{Project: "pid", Name: "Custom"}); err != nil {
		t.Fatal(err)
	}

	cfg, err := Build("/some/super-repo", pd, data)
	if err != nil {
		t.Fatal(err)
	}
	// The active name comes from state, but checkout paths are always under the
	// data dir — never configurable.
	if cfg.ProjectName != "Custom" {
		t.Fatalf("active name = %q, want Custom", cfg.ProjectName)
	}
	if cfg.MainDir != filepath.Join(data, "main") || cfg.FeaturesDir != filepath.Join(data, "features") {
		t.Fatalf("checkout dirs not derived from data dir: %+v", cfg)
	}
}

func TestBuildIdentityMismatch(t *testing.T) {
	t.Setenv(EnvHome, t.TempDir())
	data := t.TempDir()
	pd := &models.ProjectDef{ID: "pid", Name: "myslug"}
	stFile := filepath.Join(data, StateFileName)
	if err := SaveState(stFile, &models.ProjectState{Project: "a-different-uuid"}); err != nil {
		t.Fatal(err)
	}
	if _, err := Build("/some/super-repo", pd, data); err == nil {
		t.Fatal("expected identity-mismatch error")
	}
}
