package tui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/JoshuaLM114/workwood/config"
	tea "github.com/charmbracelet/bubbletea"
)

// brokenFeatureLinks flags built features with a missing/stale back-link, and
// skips features that aren't built yet.
func TestBrokenFeatureLinks(t *testing.T) {
	dataDir := t.TempDir()
	cfg := &config.Config{
		Root: t.TempDir(), DataDir: dataDir, ProjectID: "pid",
		FeaturesDir: filepath.Join(dataDir, "features"),
		StateFile:   filepath.Join(dataDir, config.StateFileName),
	}
	st := &config.ProjectState{Project: "pid", Features: map[string]config.FeatureState{}}
	st.EnsureFeature("u", "voice")
	if err := config.SaveState(cfg.StateFile, st); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(cfg.FeatureDir("voice"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := config.WriteFeatureLink(cfg, "voice"); err != nil {
		t.Fatal(err)
	}
	if b := brokenFeatureLinks(cfg); len(b) != 0 {
		t.Fatalf("valid link should not be flagged: %v", b)
	}

	// Remove the link → flagged broken.
	if err := os.RemoveAll(filepath.Dir(cfg.FeatureLinkPath("voice"))); err != nil {
		t.Fatal(err)
	}
	if b := brokenFeatureLinks(cfg); len(b) != 1 || b[0] != "voice" {
		t.Fatalf("missing link should be broken: %v", b)
	}

	// A feature with no folder on disk is skipped (not broken).
	config.WriteFeatureLink(cfg, "voice") // restore
	st.EnsureFeature("u2", "unbuilt")
	if err := config.SaveState(cfg.StateFile, st); err != nil {
		t.Fatal(err)
	}
	if b := brokenFeatureLinks(cfg); len(b) != 0 {
		t.Fatalf("unbuilt feature should be skipped: %v", b)
	}
}

// TestWindowSizeOnMenuNoPanic guards the startup crash where a WindowSizeMsg
// arrived while sitting on the root menu — the features list isn't built yet, so
// sizing it must be skipped rather than dereferencing a zero list.Model.
func TestWindowSizeOnMenuNoPanic(t *testing.T) {
	m := &Model{screen: screenMenu}
	// Would panic before the nil-guard on m.features.
	m.Update(tea.WindowSizeMsg{Width: 207, Height: 51})
	if m.width != 207 || m.height != 51 {
		t.Fatalf("size not recorded: %dx%d", m.width, m.height)
	}
}

// TestMenuNavNoPanic exercises arrow-key navigation on the root menu.
func TestMenuNavNoPanic(t *testing.T) {
	m := &Model{screen: screenMenu}
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	for range menuOrder {
		m.Update(tea.KeyMsg{Type: tea.KeyDown})
	}
	m.Update(tea.KeyMsg{Type: tea.KeyUp})
}
