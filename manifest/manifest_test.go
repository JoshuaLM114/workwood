package manifest

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "voice.yaml")
	in := &Manifest{
		Feature:     "voice",
		Description: "voice pipeline",
		Created:     "2026-06-15",
		Vars:        map[string]string{"namespace": "dev-voice"},
		Worktrees: []Worktree{
			{Repo: "api", Branch: "voice/integrate", Base: "main", Path: "voice/api"},
			{Repo: "api", Branch: "voice/experiment", Base: "main", Path: "voice/api--experiment"},
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
	if out.Vars["namespace"] != "dev-voice" {
		t.Errorf("vars lost in round-trip: %+v", out.Vars)
	}
	if idx := out.Find("api", "voice/experiment"); idx != 1 {
		t.Errorf("Find returned %d, want 1", idx)
	}
}

func TestIDProjectRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "voice.yaml")
	in := &Manifest{ID: "feat-uuid", Project: "proj-uuid", Feature: "voice"}
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

func TestNormPathStripsLegacyPrefix(t *testing.T) {
	if got := NormPath("features/voice/api"); got != "voice/api" {
		t.Errorf("NormPath = %q, want voice/api", got)
	}
}
