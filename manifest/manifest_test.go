package manifest

import (
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

func TestNormPathStripsLegacyPrefix(t *testing.T) {
	if got := NormPath("features/voice/api"); got != "voice/api" {
		t.Errorf("NormPath = %q, want voice/api", got)
	}
}
