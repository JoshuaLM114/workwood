package config

import (
	"path/filepath"
	"testing"

	"github.com/JoshuaLM114/workwood/models"
)

func TestProjectStateRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), StateFileName)

	st, err := LoadState(path) // missing file → empty, no error
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Features) != 0 {
		t.Fatalf("expected empty features, got %d", len(st.Features))
	}

	st.Project = "proj-uuid"
	st.Name = "My Project"
	st.EnsureFeature("feat-uuid", "add-login")
	st.SetWorkingSet("feat-uuid", map[string]string{"api": "/abs/api", "web": "/abs/web"})
	if err := SaveState(path, st); err != nil {
		t.Fatal(err)
	}

	got, err := LoadState(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Project != "proj-uuid" || got.Name != "My Project" {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
	fs, ok := got.FeatureByUUID("feat-uuid")
	if !ok || fs.Slug != "add-login" {
		t.Fatalf("feature not round-tripped: %+v ok=%v", fs, ok)
	}
	ws := got.WorkingSet("feat-uuid")
	if ws["api"] != "/abs/api" || ws["web"] != "/abs/web" {
		t.Fatalf("working set not round-tripped: %+v", ws)
	}
}

func TestLastPresetRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), StateFileName)
	st, _ := LoadState(path)
	st.EnsureFeature("u", "voice")
	if st.LastPreset("u") != "" {
		t.Fatal("fresh feature should have no last preset")
	}
	st.SetLastPreset("u", "voice")
	if err := SaveState(path, st); err != nil {
		t.Fatal(err)
	}
	got, _ := LoadState(path)
	if got.LastPreset("u") != "voice" {
		t.Fatalf("last preset = %q, want voice", got.LastPreset("u"))
	}
}

func TestEnsureFeatureIdempotent(t *testing.T) {
	st := &models.ProjectState{Features: map[string]models.FeatureState{}}
	if !st.EnsureFeature("u", "slug") {
		t.Fatal("first EnsureFeature should add")
	}
	if st.EnsureFeature("u", "slug") {
		t.Fatal("second EnsureFeature should be a no-op")
	}
	if st.Features["u"].Name != "slug" {
		t.Fatalf("active_name should default to slug, got %q", st.Features["u"].Name)
	}
}

func TestWorkingSetOps(t *testing.T) {
	st := &models.ProjectState{Features: map[string]models.FeatureState{}}
	st.EnsureFeature("u", "s")

	st.SetWorkingSet("u", map[string]string{"api": "/a", "web": "/w"})
	if got := st.WorkingSet("u"); len(got) != 2 || got["api"] != "/a" {
		t.Fatalf("working set not set: %+v", got)
	}
	// Empty set drops the field entirely.
	st.SetWorkingSet("u", map[string]string{})
	if st.WorkingSet("u") != nil {
		t.Fatal("empty working set should be nil")
	}
}

func TestFeatureBySlug(t *testing.T) {
	st := &models.ProjectState{Features: map[string]models.FeatureState{}}
	st.EnsureFeature("u1", "alpha")
	id, fs, ok := st.FeatureBySlug("alpha")
	if !ok || id != "u1" || fs.Slug != "alpha" {
		t.Fatalf("FeatureBySlug(alpha) = %q %+v %v", id, fs, ok)
	}
	if _, _, ok := st.FeatureBySlug("missing"); ok {
		t.Fatal("FeatureBySlug(missing) should be false")
	}
}

func TestDisplayName(t *testing.T) {
	if got := (models.FeatureState{Slug: "slug"}).DisplayName(); got != "slug" {
		t.Fatalf("want slug, got %q", got)
	}
	if got := (models.FeatureState{Slug: "slug", Name: "Nice"}).DisplayName(); got != "Nice" {
		t.Fatalf("want Nice, got %q", got)
	}
}
