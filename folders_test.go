package main

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/JoshuaLM114/workwood/i18n"
	"github.com/JoshuaLM114/workwood/manifest"
	"github.com/JoshuaLM114/workwood/models"
)

func TestFolderNameCLIPrompts(t *testing.T) {
	i18n.Init("en")
	for _, tc := range []struct {
		name, input string
		cancel      bool
		paths       []string
	}{
		{"rename", "y\ny\n", false, []string{"demo/api--one", "demo/api--two"}},
		{"default-rename", "\n\n", false, []string{"demo/api--one", "demo/api--two"}},
		{"drop", "n\nno\n", false, []string{}},
		{"mixed", "yes\nno\n", false, []string{"demo/api--one"}},
		{"invalid-answer", "maybe\ny\nn\n", false, []string{"demo/api--one"}},
		{"cancel", "cancel\n", true, nil},
		{"cancel-second", "y\nc\n", true, nil},
		{"EOF", "", true, nil},
		{"EOF-after-choice", "n\n", true, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			cfg := &models.Config{ManifestsDir: filepath.Join(root, "manifests"), FeaturesDir: filepath.Join(root, "features")}
			m := &models.Manifest{Feature: "demo", Shorthand: "d", Worktrees: []models.Worktree{
				{Repo: "api", Branch: "d/one", Path: "demo/api"},
				{Repo: "api", Branch: "d/two", Path: "demo/api-old"},
			}}
			require.NoError(t, manifest.Save(cfg.ManifestPath("demo"), m))
			before, err := os.ReadFile(cfg.ManifestPath("demo"))
			require.NoError(t, err)
			var out bytes.Buffer
			err = checkFolderNames(cfg, "demo", bufio.NewScanner(strings.NewReader(tc.input)), &out)
			require.Contains(t, out.String(), "checkout and branch are kept")
			if tc.cancel {
				require.ErrorContains(t, err, "no changes applied")
				after, err := os.ReadFile(cfg.ManifestPath("demo"))
				require.NoError(t, err)
				require.Equal(t, before, after)
				return
			}
			require.NoError(t, err)
			got, err := manifest.Load(cfg.ManifestPath("demo"))
			require.NoError(t, err)
			paths := []string{}
			for _, w := range got.Worktrees {
				paths = append(paths, w.Path)
			}
			require.Equal(t, tc.paths, paths)
			out.Reset()
			err = checkFolderNames(cfg, "demo", bufio.NewScanner(strings.NewReader("")), &out)
			require.NoError(t, err)
			require.Empty(t, out.String())
		})
	}
}

func TestFolderReviewSharesScannerWithBranchPrompt(t *testing.T) {
	i18n.Init("en")
	cfg := &models.Config{ManifestsDir: t.TempDir(), FeaturesDir: t.TempDir()}
	m := &models.Manifest{Feature: "demo", Shorthand: "d", Worktrees: []models.Worktree{{Repo: "api", Branch: "d/topic", Path: "demo/api"}}}
	require.NoError(t, manifest.Save(cfg.ManifestPath("demo"), m))
	sc := bufio.NewScanner(strings.NewReader("y\nn\n"))
	var out bytes.Buffer
	require.NoError(t, checkFolderNames(cfg, "demo", sc, &out))
	require.False(t, confirmNewBranch(sc, &out, "api", "d/topic"))
}
