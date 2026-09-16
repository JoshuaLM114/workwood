package superfeature

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/JoshuaLM114/workwood/i18n"
	"github.com/JoshuaLM114/workwood/manifest"
	"github.com/JoshuaLM114/workwood/models"
)

func addTestProject(t *testing.T) (*models.Config, *models.ProjectDef, string) {
	t.Helper()
	i18n.Init("en")
	root := t.TempDir()
	upstream := filepath.Join(root, "upstream")
	mk(t, upstream)
	git(t, upstream, "init", "-q", "-b", "main")
	git(t, upstream, "commit", "-q", "--allow-empty", "-m", "init")
	cfg := &models.Config{
		Root: root, ProjectID: "project", MainDir: filepath.Join(root, "main"),
		FeaturesDir: filepath.Join(root, "features"), ManifestsDir: filepath.Join(root, "manifests"),
	}
	mk(t, cfg.MainDir)
	git(t, root, "clone", "-q", upstream, cfg.BaseRepo("svc"))
	pd := &models.ProjectDef{Repos: []models.Repo{{Name: "svc", DefaultBranch: "main"}}}
	require.NoError(t, manifest.Save(cfg.ManifestPath("demo"), &models.Manifest{
		ID: "feature", Project: "project", Feature: "demo", Shorthand: "d",
	}))
	return cfg, pd, upstream
}

func TestAddMultipleWorktreesNamesAndRebuild(t *testing.T) {
	cfg, pd, _ := addTestProject(t)
	cases := []struct {
		sub, path string
		omit      bool
	}{
		{"fix/login", "demo/svc--fix_login", false},
		{"fix_login", "demo/svc--fix_login--2", false},
		{"other", "demo/svc--other", false},
		{"standalone", "demo/svc--standalone", true},
	}
	for _, tc := range cases {
		t.Run(tc.sub, func(t *testing.T) {
			wt, err := Add(cfg, pd, "demo", AddSpec{Repo: "svc", Sub: tc.sub, OmitFeaturePrefix: tc.omit})
			require.NoError(t, err)
			require.Equal(t, tc.path, wt.Path)
			require.Equal(t, wt.Branch, gitOut(t, cfg.Abs(wt.Path), "branch", "--show-current"))
		})
	}
	m, err := manifest.Load(cfg.ManifestPath("demo"))
	require.NoError(t, err)
	require.Len(t, m.Worktrees, len(cases))
	_, err = Down(cfg, "demo")
	require.NoError(t, err)
	_, err = Up(cfg, "demo", nil)
	require.NoError(t, err)
	for _, wt := range m.Worktrees {
		require.Equal(t, wt.Branch, gitOut(t, cfg.Abs(wt.Path), "branch", "--show-current"))
	}
}

func TestAddRejectsExistingNewBranch(t *testing.T) {
	for _, remote := range []bool{false, true} {
		for _, omit := range []bool{false, true} {
			name := "local"
			if remote {
				name = "remote"
			}
			if omit {
				name += "-standalone"
			}
			t.Run(name, func(t *testing.T) {
				cfg, pd, upstream := addTestProject(t)
				branch := ResolveBranchWith("d", "taken", omit)
				base := cfg.BaseRepo("svc")
				if remote {
					base = upstream
				}
				git(t, base, "branch", branch)
				before, err := os.ReadFile(cfg.ManifestPath("demo"))
				require.NoError(t, err)
				_, err = Add(cfg, pd, "demo", AddSpec{Repo: "svc", Sub: "taken", OmitFeaturePrefix: omit})
				require.ErrorContains(t, err, "already exists")
				after, err := os.ReadFile(cfg.ManifestPath("demo"))
				require.NoError(t, err)
				require.Equal(t, before, after)
				require.NoDirExists(t, cfg.FeatureDir("demo"))
			})
		}
	}
}

func TestAddExistingBranchMode(t *testing.T) {
	for _, source := range []string{"local", "remote", "missing"} {
		t.Run(source, func(t *testing.T) {
			cfg, pd, upstream := addTestProject(t)
			if source == "local" {
				git(t, cfg.BaseRepo("svc"), "branch", "topic")
			}
			if source == "remote" {
				git(t, upstream, "branch", "topic")
			}
			wt, err := Add(cfg, pd, "demo", AddSpec{Repo: "svc", Sub: "topic", ExistingBranch: true})
			if source == "missing" {
				require.ErrorContains(t, err, "does not exist")
				require.False(t, branchExists(cfg.BaseRepo("svc"), "topic"))
				return
			}
			require.NoError(t, err)
			require.Equal(t, "topic", wt.Branch)
			require.Equal(t, "demo/svc--topic", wt.Path)
			if source == "remote" {
				require.Equal(t, "origin/topic", gitOut(t, cfg.Abs(wt.Path), "rev-parse", "--abbrev-ref", "@{u}"))
			}
			_, err = Add(cfg, pd, "demo", AddSpec{Repo: "svc", Sub: "topic", ExistingBranch: true})
			require.ErrorContains(t, err, "already has a worktree")
		})
	}
}

func TestAddRejectsInvalidNames(t *testing.T) {
	cfg, pd, _ := addTestProject(t)
	for _, name := range []string{"", "bad name", "../escape", "-option", "HEAD", "@{-1}", "topic.lock"} {
		t.Run(name, func(t *testing.T) {
			_, err := Add(cfg, pd, "demo", AddSpec{Repo: "svc", Sub: name, OmitFeaturePrefix: true})
			require.ErrorContains(t, err, "invalid branch name")
		})
	}
}

func TestAllocPathAvoidsReservedAndOccupiedPaths(t *testing.T) {
	for _, kind := range []string{"manifest", "directory", "file", "broken-symlink"} {
		t.Run(kind, func(t *testing.T) {
			cfg := &models.Config{FeaturesDir: t.TempDir()}
			m := &models.Manifest{Feature: "demo", Shorthand: "d"}
			rel := "demo/svc--topic"
			mk(t, cfg.FeatureDir("demo"))
			switch kind {
			case "manifest":
				m.Worktrees = []models.Worktree{{Path: rel}, {Path: rel + "--2"}}
			case "directory":
				mk(t, cfg.Abs(rel))
			case "file":
				require.NoError(t, os.WriteFile(cfg.Abs(rel), []byte("keep"), 0o644))
			case "broken-symlink":
				require.NoError(t, os.Symlink("missing", cfg.Abs(rel)))
			}
			want := rel + "--2"
			if kind == "manifest" {
				want = rel + "--3"
			}
			require.Equal(t, want, allocPath(cfg, m, "svc", "d/topic"))
		})
	}
}

func TestRecordedBranchReservedAndLegacyPathRequiresReview(t *testing.T) {
	cfg, pd, _ := addTestProject(t)
	m, err := manifest.Load(cfg.ManifestPath("demo"))
	require.NoError(t, err)
	m.Worktrees = []models.Worktree{{Repo: "svc", Branch: "d/legacy", Base: "main", Path: "demo/svc"}}
	require.NoError(t, manifest.Save(cfg.ManifestPath("demo"), m))
	_, err = Add(cfg, pd, "demo", AddSpec{Repo: "svc", Sub: "legacy"})
	require.ErrorContains(t, err, "already has a worktree")
	_, err = Up(cfg, "demo", nil)
	require.ErrorContains(t, err, "outdated worktree folder names")
	changes, err := CheckFolderNames(cfg, "demo")
	require.NoError(t, err)
	require.Len(t, changes, 1)
	_, err = ApplyFolderNames(cfg, "demo", []FolderDecision{{FolderChange: changes[0], Rename: true}})
	require.NoError(t, err)
	_, err = Up(cfg, "demo", nil)
	require.NoError(t, err)
	require.Equal(t, "d/legacy", gitOut(t, cfg.Abs("demo/svc--legacy"), "branch", "--show-current"))
	wt, err := Add(cfg, pd, "demo", AddSpec{Repo: "svc", Sub: "next"})
	require.NoError(t, err)
	require.Equal(t, "demo/svc--next", wt.Path)
}

func TestApplyEditRejectsDuplicateAddition(t *testing.T) {
	cfg, pd, _ := addTestProject(t)
	spec := AddSpec{Repo: "svc", Sub: "topic"}
	result, err := ApplyEdit(cfg, pd, "demo", "", []AddSpec{spec, spec}, nil)
	require.ErrorContains(t, err, "already has a worktree")
	require.Len(t, result.Added, 1)
	m, err := manifest.Load(cfg.ManifestPath("demo"))
	require.NoError(t, err)
	require.Len(t, m.Worktrees, 1)
}
