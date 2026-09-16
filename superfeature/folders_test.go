package superfeature

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/JoshuaLM114/workwood/config"
	"github.com/JoshuaLM114/workwood/libs/gitx"
	"github.com/JoshuaLM114/workwood/manifest"
	"github.com/JoshuaLM114/workwood/models"
	"github.com/JoshuaLM114/workwood/targetcfg"
)

func legacyFolderProject(t *testing.T, branches ...string) (*models.Config, []FolderDecision) {
	t.Helper()
	cfg, _, _ := addTestProject(t)
	cfg.StateDir = cfg.Root
	cfg.StateFile = filepath.Join(cfg.Root, "state.yml")
	m, err := manifest.Load(cfg.ManifestPath("demo"))
	require.NoError(t, err)
	for i, branch := range branches {
		w := models.Worktree{Repo: "svc", Branch: "d/" + branch, Base: "main", Path: fmt.Sprintf("demo/old-%d", i)}
		git(t, cfg.BaseRepo("svc"), "worktree", "add", "-b", w.Branch, cfg.Abs(w.Path), "main")
		m.Worktrees = append(m.Worktrees, w)
	}
	require.NoError(t, manifest.Save(cfg.ManifestPath("demo"), m))
	changes, err := CheckFolderNames(cfg, "demo")
	require.NoError(t, err)
	var decisions []FolderDecision
	for _, change := range changes {
		decisions = append(decisions, FolderDecision{FolderChange: change, Rename: true})
	}
	return cfg, decisions
}

func TestCheckFolderNames(t *testing.T) {
	cfg, _, _ := addTestProject(t)
	for _, tc := range []struct {
		branch, path string
		valid        bool
	}{
		{"d/fix-login", "demo/svc--fix-login", true},
		{"d/fix/login", "demo/svc--fix_login", true},
		{"d/fix-login", "demo/svc--fix-login--2", true},
		{"d/fix-login", "demo/svc--fix-login--42", true},
		{"other/topic", "demo/svc--other_topic", true},
		{"d/fix-login", "demo/svc", false},
		{"d/fix-login", "demo/svc--d_fix-login", false},
		{"d/fix-login", "demo/svc--fix-login--1", false},
		{"d/fix-login", "demo/svc--fix-login--02", false},
		{"d/fix-login", "other/svc--fix-login", false},
	} {
		t.Run(tc.path, func(t *testing.T) {
			m := &models.Manifest{Feature: "demo", Shorthand: "d", Worktrees: []models.Worktree{{Repo: "svc", Branch: tc.branch, Path: tc.path}}}
			require.NoError(t, manifest.Save(cfg.ManifestPath("demo"), m))
			changes, err := CheckFolderNames(cfg, "demo")
			require.NoError(t, err)
			if tc.valid {
				require.Empty(t, changes)
			} else {
				require.Len(t, changes, 1)
				require.True(t, changes[0].Missing)
			}
		})
	}
}

func TestRenameFoldersPreservesDirtyCheckoutAndTargets(t *testing.T) {
	cfg, decisions := legacyFolderProject(t, "fix/login", "fix_login")
	require.Equal(t, "demo/svc--fix_login", decisions[0].Path)
	require.Equal(t, "demo/svc--fix_login--2", decisions[1].Path)
	oldPath := cfg.Abs(decisions[0].Worktree.Path)
	newPath := cfg.Abs(decisions[0].Path)
	require.NoError(t, os.WriteFile(filepath.Join(oldPath, "tracked"), []byte("initial"), 0o644))
	git(t, oldPath, "add", "tracked")
	git(t, oldPath, "commit", "-m", "tracked file")
	require.NoError(t, os.WriteFile(filepath.Join(oldPath, "tracked"), []byte("staged"), 0o644))
	git(t, oldPath, "add", "tracked")
	require.NoError(t, os.WriteFile(filepath.Join(oldPath, "tracked"), []byte("unstaged"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(oldPath, "notes"), []byte("untracked"), 0o644))
	beforeStatus := gitOut(t, oldPath, "status", "--porcelain")
	beforeIndex := gitOut(t, oldPath, "ls-files", "--stage")
	beforeHead := gitOut(t, oldPath, "rev-parse", "HEAD")
	set := models.Set{"root": oldPath, "service": filepath.Join(oldPath, "services", "api"), "sibling": oldPath + "-other", "base": cfg.BaseRepo("svc")}
	st := &models.ProjectState{Features: map[string]models.FeatureState{"feature": {Slug: "demo", Targets: set}}}
	require.NoError(t, config.SaveState(cfg.StateFile, st))
	require.NoError(t, targetcfg.SavePreset(cfg, "custom", set))
	_, err := ApplyFolderNames(cfg, "demo", decisions)
	require.NoError(t, err)
	require.NoDirExists(t, oldPath)
	require.Equal(t, beforeStatus, gitOut(t, newPath, "status", "--porcelain"))
	require.Equal(t, beforeIndex, gitOut(t, newPath, "ls-files", "--stage"))
	require.Equal(t, beforeHead, gitOut(t, newPath, "rev-parse", "HEAD"))
	require.Equal(t, "d/fix/login", gitOut(t, newPath, "branch", "--show-current"))
	resolvedPath, err := filepath.EvalSymlinks(newPath)
	require.NoError(t, err)
	require.Contains(t, gitOut(t, cfg.BaseRepo("svc"), "worktree", "list", "--porcelain"), "worktree "+resolvedPath+"\n")
	gotState, err := config.LoadState(cfg.StateFile)
	require.NoError(t, err)
	gotPreset, err := targetcfg.LoadPreset(cfg, "custom")
	require.NoError(t, err)
	for _, got := range []models.Set{gotState.WorkingSet("feature"), gotPreset} {
		require.Equal(t, models.Set{"root": newPath, "service": filepath.Join(newPath, "services", "api"), "sibling": oldPath + "-other", "base": cfg.BaseRepo("svc")}, got)
	}
	changes, err := CheckFolderNames(cfg, "demo")
	require.NoError(t, err)
	require.Empty(t, changes)
}

func TestDeclinedFolderSurvivesUpDownAndDelete(t *testing.T) {
	cfg, decisions := legacyFolderProject(t, "keep", "rename")
	decisions[0].Rename = false
	kept := cfg.Abs(decisions[0].Worktree.Path)
	require.NoError(t, os.WriteFile(filepath.Join(kept, "notes"), []byte("keep this"), 0o644))
	require.NoError(t, targetcfg.SavePreset(cfg, "custom", models.Set{"kept": kept}))
	_, err := ApplyFolderNames(cfg, "demo", decisions)
	require.NoError(t, err)
	m, err := manifest.Load(cfg.ManifestPath("demo"))
	require.NoError(t, err)
	require.Len(t, m.Worktrees, 1)
	require.Equal(t, "d/rename", m.Worktrees[0].Branch)
	set, err := targetcfg.LoadPreset(cfg, "custom")
	require.NoError(t, err)
	require.Equal(t, kept, set["kept"])
	_, err = Up(cfg, "demo", nil)
	require.NoError(t, err)
	_, err = Down(cfg, "demo")
	require.NoError(t, err)
	require.FileExists(t, filepath.Join(kept, "notes"))
	require.Equal(t, "d/keep", gitOut(t, kept, "branch", "--show-current"))
	err = Delete(cfg, "demo", true)
	require.NoError(t, err)
	require.FileExists(t, filepath.Join(kept, "notes"))
	require.Equal(t, "d/keep", gitOut(t, kept, "branch", "--show-current"))
	require.True(t, branchExists(cfg.BaseRepo("svc"), "d/keep"))
}

func TestFolderRenameRejectsChangedReview(t *testing.T) {
	for _, changed := range []string{"destination", "manifest", "missing"} {
		t.Run(changed, func(t *testing.T) {
			cfg, decisions := legacyFolderProject(t, "topic")
			switch changed {
			case "destination":
				mk(t, cfg.Abs(decisions[0].Path))
			case "manifest":
				m, err := manifest.Load(cfg.ManifestPath("demo"))
				require.NoError(t, err)
				m.Worktrees[0].Branch = "d/other"
				require.NoError(t, manifest.Save(cfg.ManifestPath("demo"), m))
			case "missing":
				decisions[0].Missing = true
			}
			_, err := ApplyFolderNames(cfg, "demo", decisions)
			require.ErrorContains(t, err, "changed during review")
			require.DirExists(t, cfg.Abs(decisions[0].Worktree.Path))
		})
	}
}

func TestFolderRenameRollsBackFailedMoveOrWrite(t *testing.T) {
	for _, failure := range []string{"locked", "manifest-write"} {
		t.Run(failure, func(t *testing.T) {
			if failure == "manifest-write" && os.Geteuid() == 0 {
				t.Skip("root can write read-only directories")
			}
			cfg, decisions := legacyFolderProject(t, "one", "two")
			oldPath := cfg.Abs(decisions[0].Worktree.Path)
			require.NoError(t, targetcfg.SavePreset(cfg, "custom", models.Set{"root": oldPath}))
			manifestBefore, err := os.ReadFile(cfg.ManifestPath("demo"))
			require.NoError(t, err)
			presetBefore, err := os.ReadFile(targetcfg.PresetPath(cfg, "custom"))
			require.NoError(t, err)
			switch failure {
			case "locked":
				git(t, cfg.BaseRepo("svc"), "worktree", "lock", cfg.Abs(decisions[1].Worktree.Path))
			case "manifest-write":
				require.NoError(t, os.Chmod(cfg.ManifestsDir, 0o555))
				defer os.Chmod(cfg.ManifestsDir, 0o755)
			}
			_, err = ApplyFolderNames(cfg, "demo", decisions)
			require.Error(t, err)
			manifestAfter, err := os.ReadFile(cfg.ManifestPath("demo"))
			require.NoError(t, err)
			require.Equal(t, manifestBefore, manifestAfter)
			presetAfter, err := os.ReadFile(targetcfg.PresetPath(cfg, "custom"))
			require.NoError(t, err)
			require.Equal(t, presetBefore, presetAfter)
			for _, d := range decisions {
				require.NoDirExists(t, cfg.Abs(d.Path))
				require.Equal(t, d.Worktree.Branch, gitOut(t, cfg.Abs(d.Worktree.Path), "branch", "--show-current"))
			}
		})
	}
}

func TestFolderRenameRefusesWrongBranchOrSymlink(t *testing.T) {
	for _, failure := range []string{"branch", "symlink", "feature-symlink", "outside-feature"} {
		t.Run(failure, func(t *testing.T) {
			cfg, decisions := legacyFolderProject(t, "topic")
			oldPath := cfg.Abs(decisions[0].Worktree.Path)
			switch failure {
			case "branch":
				git(t, oldPath, "switch", "-c", "other")
			case "symlink":
				require.NoError(t, os.Rename(oldPath, oldPath+"-moved"))
				require.NoError(t, os.Symlink(oldPath+"-moved", oldPath))
			case "feature-symlink":
				require.NoError(t, os.Rename(cfg.FeatureDir("demo"), cfg.FeatureDir("demo")+"-moved"))
				require.NoError(t, os.Symlink(cfg.FeatureDir("demo")+"-moved", cfg.FeatureDir("demo")))
			case "outside-feature":
				m, err := manifest.Load(cfg.ManifestPath("demo"))
				require.NoError(t, err)
				m.Worktrees[0].Path = "demo/../main/svc"
				require.NoError(t, manifest.Save(cfg.ManifestPath("demo"), m))
				changes, err := CheckFolderNames(cfg, "demo")
				require.NoError(t, err)
				decisions[0].FolderChange = changes[0]
			}
			_, err := ApplyFolderNames(cfg, "demo", decisions)
			require.Error(t, err)
			require.NoDirExists(t, cfg.Abs(decisions[0].Path))
		})
	}
}

func TestMoveWorktreeRefusesOccupiedDestination(t *testing.T) {
	cfg, decisions := legacyFolderProject(t, "topic")
	from, to := cfg.Abs(decisions[0].Worktree.Path), cfg.Abs(decisions[0].Path)
	mk(t, to)
	require.ErrorContains(t, gitx.MoveWorktree(cfg.BaseRepo("svc"), from, to), "already exists")
	require.Equal(t, "d/topic", gitOut(t, from, "branch", "--show-current"))
	entries, err := os.ReadDir(to)
	require.NoError(t, err)
	require.Empty(t, entries, "an occupied destination must not receive a nested checkout")
}
