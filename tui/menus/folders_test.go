package menus

import (
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/stretchr/testify/require"

	"github.com/JoshuaLM114/workwood/i18n"
	"github.com/JoshuaLM114/workwood/manifest"
	"github.com/JoshuaLM114/workwood/models"
)

func TestFolderNamesReview(t *testing.T) {
	i18n.Init("en")
	for _, choice := range []string{"rename", "drop", "cancel", "cancel-after-choice"} {
		t.Run(choice, func(t *testing.T) {
			root := t.TempDir()
			cfg := &models.Config{ManifestsDir: filepath.Join(root, "manifests"), FeaturesDir: filepath.Join(root, "features")}
			require.NoError(t, manifest.Save(cfg.ManifestPath("demo"), &models.Manifest{
				Feature: "demo", Shorthand: "d", Worktrees: []models.Worktree{
					{Repo: "api", Branch: "d/one", Path: "demo/api"},
					{Repo: "api", Branch: "d/two", Path: "demo/api-old"},
				},
			}))
			next := OpenEditorMsg{Feature: "demo"}
			m, err := NewFolderNames(Ctx{Cfg: cfg, Width: 100, Height: 40}, "demo", next)
			require.NoError(t, err)
			require.NotNil(t, m)
			require.Len(t, m.decisions, 2)
			if choice == "cancel-after-choice" {
				m.decisions[0].Rename = false
			}
			if choice == "cancel" || choice == "cancel-after-choice" {
				before, err := os.ReadFile(cfg.ManifestPath("demo"))
				require.NoError(t, err)
				_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
				require.IsType(t, BackMsg{}, cmd())
				after, err := os.ReadFile(cfg.ManifestPath("demo"))
				require.NoError(t, err)
				require.Equal(t, before, after)
				return
			}
			for i := range m.decisions {
				m.decisions[i].Rename = choice == "rename"
			}
			m.form.State = huh.StateCompleted
			_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
			require.True(t, m.busy)
			batch := cmd().(tea.BatchMsg)
			require.Len(t, batch, 2)
			_, _ = m.Update(batch[1]())
			require.True(t, m.done)
			require.False(t, m.busy)
			require.NoError(t, m.err)
			_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
			require.Equal(t, next, cmd())
			got, err := manifest.Load(cfg.ManifestPath("demo"))
			require.NoError(t, err)
			if choice == "drop" {
				require.Empty(t, got.Worktrees)
			} else {
				require.Equal(t, "demo/api--one", got.Worktrees[0].Path)
				require.Equal(t, "demo/api--two", got.Worktrees[1].Path)
			}
			m, err = NewFolderNames(Ctx{Cfg: cfg}, "demo", next)
			require.NoError(t, err)
			require.Nil(t, m)
		})
	}
}
