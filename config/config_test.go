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
	if cfg.StateDir != filepath.Join(data, "pid") {
		t.Fatalf("state dir: %q", cfg.StateDir)
	}
	if cfg.MainDir != filepath.Join(data, "pid", "main") || cfg.FeaturesDir != filepath.Join(data, "pid", "features") {
		t.Fatalf("default checkout dirs wrong: main=%q features=%q", cfg.MainDir, cfg.FeaturesDir)
	}
	if cfg.PluginsDir != filepath.Join("/some/super-repo", "workwood", "plugins") {
		t.Fatalf("plugins dir: %q", cfg.PluginsDir)
	}
}

func TestBuildStateOverrides(t *testing.T) {
	t.Setenv(EnvHome, t.TempDir())
	data := t.TempDir()
	pd := &projectdef.File{ID: "pid", Name: "myslug"}
	stFile := filepath.Join(data, "pid", StateFileName)
	if err := SaveState(stFile, &ProjectState{
		Project: "pid", Name: "Custom", MainDir: "/custom/main", FeaturesDir: "/custom/feat",
	}); err != nil {
		t.Fatal(err)
	}

	cfg, err := Build("/some/super-repo", pd, data)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ProjectName != "Custom" || cfg.MainDir != "/custom/main" || cfg.FeaturesDir != "/custom/feat" {
		t.Fatalf("overrides not applied: %+v", cfg)
	}
}

func TestBuildIdentityMismatch(t *testing.T) {
	t.Setenv(EnvHome, t.TempDir())
	data := t.TempDir()
	pd := &projectdef.File{ID: "pid", Name: "myslug"}
	stFile := filepath.Join(data, "pid", StateFileName)
	if err := SaveState(stFile, &ProjectState{Project: "a-different-uuid"}); err != nil {
		t.Fatal(err)
	}
	if _, err := Build("/some/super-repo", pd, data); err == nil {
		t.Fatal("expected identity-mismatch error")
	}
}
