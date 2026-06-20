package config

import (
	"path/filepath"
	"testing"

	"github.com/JoshuaLM114/workwood/targets"
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
	st.MainDir = "/m"
	st.FeaturesDir = "/f"
	st.EnsureFeature("feat-uuid", "add-login")
	if !st.AddTarget("feat-uuid", "api", targets.Target{Source: targets.SourceWorktree}) {
		t.Fatal("AddTarget should report newly added")
	}
	if err := SaveState(path, st); err != nil {
		t.Fatal(err)
	}

	got, err := LoadState(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Project != "proj-uuid" || got.Name != "My Project" || got.MainDir != "/m" {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
	fs, ok := got.FeatureByUUID("feat-uuid")
	if !ok || fs.Slug != "add-login" {
		t.Fatalf("feature not round-tripped: %+v ok=%v", fs, ok)
	}
	if len(fs.Targets["api"]) != 1 || fs.Targets["api"][0].Source != targets.SourceWorktree {
		t.Fatalf("targets not round-tripped: %+v", fs.Targets)
	}
}

func TestEnsureFeatureIdempotent(t *testing.T) {
	st := &ProjectState{Features: map[string]FeatureState{}}
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

func TestTargetOps(t *testing.T) {
	st := &ProjectState{Features: map[string]FeatureState{}}
	st.EnsureFeature("u", "s")
	tw := targets.Target{Source: targets.SourceWorktree}

	if !st.AddTarget("u", "api", tw) {
		t.Fatal("first add should be true")
	}
	if st.AddTarget("u", "api", tw) {
		t.Fatal("duplicate add should be false")
	}
	if len(st.TargetsFor("u", "api")) != 1 {
		t.Fatalf("want 1 target, got %d", len(st.TargetsFor("u", "api")))
	}
	if !st.RemoveTarget("u", "api", tw) {
		t.Fatal("remove should be true")
	}
	if st.RemoveTarget("u", "api", tw) {
		t.Fatal("second remove should be false")
	}
	st.AddTarget("u", "web", tw)
	st.ClearTarget("u", "web")
	if len(st.TargetsFor("u", "web")) != 0 {
		t.Fatal("clear should empty the repo's setups")
	}
}

func TestFeatureBySlug(t *testing.T) {
	st := &ProjectState{Features: map[string]FeatureState{}}
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
	if got := (FeatureState{Slug: "slug"}).DisplayName(); got != "slug" {
		t.Fatalf("want slug, got %q", got)
	}
	if got := (FeatureState{Slug: "slug", Name: "Nice"}).DisplayName(); got != "Nice" {
		t.Fatalf("want Nice, got %q", got)
	}
}
