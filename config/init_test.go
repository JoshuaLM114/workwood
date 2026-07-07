package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JoshuaLM114/workwood/manifest"
	"github.com/JoshuaLM114/workwood/models"
)

func TestInitProject(t *testing.T) {
	root := t.TempDir()
	dataDir := filepath.Join(root, "data") // inside the repo → should be gitignored
	pd := &models.ProjectDef{ID: "proj-uuid", Name: "demo"}

	// Pre-place a committed manifest so the tracking/back-fill path runs.
	mdir := filepath.Join(root, WorkwoodDirName, ManifestsDirName)
	if err := os.MkdirAll(mdir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := manifest.Save(filepath.Join(mdir, "feat.yaml"), &models.Manifest{ID: "feat-uuid", Project: pd.ID, Feature: "feat"}); err != nil {
		t.Fatal(err)
	}

	res, err := InitProject(root, pd, dataDir)
	if err != nil {
		t.Fatalf("InitProject: %v", err)
	}

	for _, d := range []string{res.ActionsDir, res.ManifestsDir} {
		if _, err := os.Stat(d); err != nil {
			t.Errorf("dir %s not created: %v", d, err)
		}
	}
	if res.Tracked != 1 {
		t.Errorf("Tracked = %d, want 1", res.Tracked)
	}
	st, err := LoadState(res.StateFile)
	if err != nil {
		t.Fatal(err)
	}
	if st.Project != pd.ID {
		t.Errorf("state.Project = %q, want %q", st.Project, pd.ID)
	}
	if _, ok := st.Features["feat-uuid"]; !ok {
		t.Errorf("feature not tracked in state: %+v", st.Features)
	}
	gi, _ := os.ReadFile(filepath.Join(root, ".gitignore"))
	if !strings.Contains(string(gi), "/data/") {
		t.Errorf(".gitignore missing data-dir entry, got %q", gi)
	}

	// A second run with a DIFFERENT project id must be rejected (identity guard).
	if _, err := InitProject(root, &models.ProjectDef{ID: "other"}, dataDir); err == nil {
		t.Error("InitProject with mismatched project id = nil, want error")
	}
}
