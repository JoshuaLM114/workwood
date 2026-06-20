package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/JoshuaLM114/workwood/projectdef"
)

func TestFindProjectRoot(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ProjectDefName), []byte("id: x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := findProjectRoot(sub); got != root {
		t.Fatalf("walk-up: got %q want %q", got, root)
	}
	if got := findProjectRoot(root); got != root {
		t.Fatalf("at-root: got %q want %q", got, root)
	}
	if got := findProjectRoot(t.TempDir()); got != "" {
		t.Fatalf("no project: got %q want \"\"", got)
	}
}

func TestBuildDefaults(t *testing.T) {
	t.Setenv(EnvHome, t.TempDir())
	data := t.TempDir()
	pd := &projectdef.File{ID: "pid", Name: "myslug"}

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
	pd := &projectdef.File{ID: "pid", Name: "myslug"}
	stFile := filepath.Join(data, StateFileName)
	if err := SaveState(stFile, &ProjectState{Project: "pid", Name: "Custom"}); err != nil {
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
	pd := &projectdef.File{ID: "pid", Name: "myslug"}
	stFile := filepath.Join(data, StateFileName)
	if err := SaveState(stFile, &ProjectState{Project: "a-different-uuid"}); err != nil {
		t.Fatal(err)
	}
	if _, err := Build("/some/super-repo", pd, data); err == nil {
		t.Fatal("expected identity-mismatch error")
	}
}
