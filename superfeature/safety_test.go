package superfeature

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/JoshuaLM114/workwood/config"
	"github.com/JoshuaLM114/workwood/manifest"
)

func TestValidName(t *testing.T) {
	bad := []string{"", ".", "..", "a/b", `a\b`, "-x", "../x", "x/..", "/abs", "../../etc"}
	for _, s := range bad {
		if err := validName(s); err == nil {
			t.Errorf("validName(%q) = nil, want error", s)
		}
	}
	good := []string{"feature", "my-feature", "repo_1", "a.b", "v2", "sample-db"}
	for _, s := range good {
		if err := validName(s); err != nil {
			t.Errorf("validName(%q) = %v, want nil", s, err)
		}
	}
}

func TestUnderData(t *testing.T) {
	cfg := &config.Config{FeaturesDir: "/data/features"}
	inside := []string{"/data/features/feat/repo", "/data/features/x"}
	for _, p := range inside {
		if !underData(cfg, p) {
			t.Errorf("underData(%q) = false, want true", p)
		}
	}
	// "." (the features dir itself), parents, escapes, and sibling prefixes.
	outside := []string{"/data/features", "/data", "/data/features/../../etc", "/etc/passwd", "/data/featuresX"}
	for _, p := range outside {
		if underData(cfg, p) {
			t.Errorf("underData(%q) = true, want false", p)
		}
	}
}

func TestSafeRemoveAll(t *testing.T) {
	root := t.TempDir()
	cfg := &config.Config{FeaturesDir: filepath.Join(root, "features")}

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
	cfg := new(config.Config) // validName runs first; cfg is never touched
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
	cfg := &config.Config{ManifestsDir: t.TempDir(), FeaturesDir: t.TempDir()}
	// No manifest on disk, so each op fails — but Reconcile must continue and
	// return exactly one outcome per planned item, never aborting on the first.
	plan := ReconcilePlan{
		DropMissing: []manifest.Worktree{{Repo: "a", Branch: "x"}, {Repo: "b", Branch: "y"}},
	}
	if got := Reconcile(cfg, "feat", plan); len(got) != 2 {
		t.Fatalf("Reconcile returned %d outcomes, want 2", len(got))
	}
	if got := Reconcile(cfg, "feat", ReconcilePlan{}); len(got) != 0 {
		t.Errorf("empty plan → %d outcomes, want 0", len(got))
	}
}
