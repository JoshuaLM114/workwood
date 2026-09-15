package manifest

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/JoshuaLM114/workwood/models"
)

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "demo.yaml")
	in := &models.Manifest{
		Feature:     "demo",
		Description: "sample feature",
		Created:     "2026-06-15",
		Vars:        map[string]string{"namespace": "dev-demo"},
		Worktrees: []models.Worktree{
			{Repo: "api", Branch: "demo/integrate", Base: "main", Path: "demo/api"},
			{Repo: "api", Branch: "demo/experiment", Base: "main", Path: "demo/api--experiment"},
		},
	}
	if err := Save(path, in); err != nil {
		t.Fatalf("Save: %v", err)
	}
	out, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if out.Version == 0 {
		t.Errorf("expected Save to stamp a version, got 0")
	}
	if out.Feature != in.Feature || out.Description != in.Description || len(out.Worktrees) != 2 {
		t.Fatalf("round-trip mismatch: %+v", out)
	}
	if out.Vars["namespace"] != "dev-demo" {
		t.Errorf("vars lost in round-trip: %+v", out.Vars)
	}
	if idx := out.Find("api", "demo/experiment"); idx != 1 {
		t.Errorf("Find returned %d, want 1", idx)
	}
}

func TestIDProjectRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "demo.yaml")
	in := &models.Manifest{ID: "feat-uuid", Project: "proj-uuid", Feature: "demo"}
	if err := Save(path, in); err != nil {
		t.Fatal(err)
	}
	out, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if out.ID != "feat-uuid" || out.Project != "proj-uuid" {
		t.Fatalf("id/project not round-tripped: %+v", out)
	}
}

// A legacy manifest predating UUIDs must still load (id/project absent, no error).
func TestLoadBackCompatNoUUID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.yaml")
	legacy := "version: 1\nfeature: legacy\ndescription: old\nworktrees: []\n"
	if err := os.WriteFile(path, []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := Load(path)
	if err != nil {
		t.Fatalf("legacy manifest should load: %v", err)
	}
	if out.Feature != "legacy" || out.ID != "" || out.Project != "" {
		t.Fatalf("unexpected: %+v", out)
	}
}

func TestDefaultShorthand(t *testing.T) {
	cases := map[string]string{
		"my-new-super-feature": "mnsf",
		"add_login_flow":       "alf",
		"two words":            "tw",
		"demo":                 "d", // single word → its first letter
		"":                     "",
	}
	for in, want := range cases {
		if got := DefaultShorthand(in); got != want {
			t.Errorf("DefaultShorthand(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestBranchPrefix(t *testing.T) {
	if got := (&models.Manifest{Feature: "demo", Shorthand: "d"}).BranchPrefix(); got != "d" {
		t.Errorf("with shorthand: got %q, want d", got)
	}
	if got := (&models.Manifest{Feature: "demo"}).BranchPrefix(); got != "demo" {
		t.Errorf("no shorthand: got %q, want demo (fallback to feature)", got)
	}
}

func TestNormPathStripsLegacyPrefix(t *testing.T) {
	if got := NormPath("features/demo/api"); got != "demo/api" {
		t.Errorf("NormPath = %q, want demo/api", got)
	}
}
