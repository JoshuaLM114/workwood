package config

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/JoshuaLM114/workwood/manifest"
	"github.com/JoshuaLM114/workwood/models"
	"github.com/JoshuaLM114/workwood/projectdef"
)

func setupFiles(t *testing.T, root string) map[string]string {
	t.Helper()
	files := map[string]string{}
	require.NoError(t, filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			contents, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			files[path] = string(contents)
		}
		return nil
	}))
	return files
}

func TestDetectAndUpgradeLegacyProject(t *testing.T) {
	for _, id := range []string{"", "project-id"} {
		t.Run("identity="+id, func(t *testing.T) {
			base := t.TempDir()
			home, root, data := filepath.Join(base, "home"), filepath.Join(base, "super"), filepath.Join(base, "data")
			t.Setenv(EnvHome, home)
			t.Setenv(EnvData, "")
			nested := filepath.Join(root, "nested", "directory")
			require.NoError(t, os.MkdirAll(nested, 0o755))
			pd := &models.ProjectDef{ID: id, Name: "demo", Repos: []models.Repo{{Name: "api", URL: "./api", DefaultBranch: "main"}}}
			require.NoError(t, projectdef.Save(filepath.Join(root, ProjectDefName), pd))
			cfg, err := Build(root, pd, data)
			require.NoError(t, err)
			checkout := cfg.Abs("login/api")
			require.NoError(t, os.MkdirAll(checkout, 0o755))
			require.NoError(t, os.WriteFile(filepath.Join(checkout, "changes.txt"), []byte("local edits"), 0o644))
			m := &models.Manifest{ID: "feature-id", Feature: "login", Worktrees: []models.Worktree{{Repo: "api", Branch: "sf/fix-login", Path: "login/api"}}}
			require.NoError(t, manifest.Save(cfg.ManifestPath(m.Feature), m))
			require.NoError(t, manifest.Save(cfg.ManifestPath("legacy"), &models.Manifest{Feature: "legacy"}))
			localFeature := models.FeatureState{Slug: "login", Name: "My login work", Targets: map[string]string{"custom-api": checkout}, WorkingSetConfigured: true, LastPreset: "saved"}
			legacyState := models.FeatureState{Slug: "legacy", Name: "Legacy work", WorkingSetConfigured: true}
			require.NoError(t, SaveState(cfg.StateFile, &models.ProjectState{Project: id, Name: "My project", Features: map[string]models.FeatureState{m.ID: localFeature, "legacy-id": legacyState}}))
			require.NoError(t, os.MkdirAll(cfg.ActionsDir, 0o755))
			script := filepath.Join(cfg.ActionsDir, "check")
			require.NoError(t, os.WriteFile(script, []byte("Run() { :; }\nValidate() { :; }\n"), 0o755))
			presets := filepath.Join(data, "targets")
			require.NoError(t, os.MkdirAll(presets, 0o755))
			require.NoError(t, os.WriteFile(filepath.Join(presets, "saved.yml"), []byte("custom-api: "+checkout+"\n"), 0o644))
			t.Chdir(nested)

			before := setupFiles(t, base)
			status, err := DetectProject("", data)
			require.NoError(t, err)
			require.Equal(t, "needs_init", status.Status)
			require.True(t, status.Found)
			require.False(t, status.Registered)
			require.Equal(t, root, status.Root)
			require.Equal(t, "argument", status.DataDirSource)
			require.Equal(t, &InitArguments{Path: root, DataDir: data}, status.InitArguments)
			require.Equal(t, before, setupFiles(t, base), "detection stays read-only")

			result, err := InitializeProject("", data, "ignored")
			require.NoError(t, err)
			require.Equal(t, "ready", result.Status.Status)
			require.True(t, result.Status.Registered)
			require.Equal(t, SetupVersion, result.Status.SetupVersion)
			require.Empty(t, result.Status.Issues)
			require.Equal(t, root, result.Project.Root)
			require.NoFileExists(t, filepath.Join(nested, ProjectDefName))
			updated, err := projectdef.Load(cfg.ProjectDef)
			require.NoError(t, err)
			require.NotEmpty(t, updated.ID)
			require.Equal(t, pd.Name, updated.Name)
			require.Equal(t, pd.Repos, updated.Repos)
			if id != "" {
				require.Equal(t, id, updated.ID)
			}
			updatedFeature, err := manifest.Load(cfg.ManifestPath(m.Feature))
			require.NoError(t, err)
			require.Equal(t, m.ID, updatedFeature.ID)
			require.Equal(t, updated.ID, updatedFeature.Project)
			require.Equal(t, m.Worktrees, updatedFeature.Worktrees)
			st, err := LoadState(cfg.StateFile)
			require.NoError(t, err)
			require.Equal(t, "My project", st.Name)
			require.Equal(t, localFeature, st.Features[m.ID])
			require.Equal(t, legacyState, st.Features["legacy-id"])
			legacy, err := manifest.Load(cfg.ManifestPath("legacy"))
			require.NoError(t, err)
			require.Equal(t, "legacy-id", legacy.ID)
			after := setupFiles(t, base)
			for _, path := range []string{script, filepath.Join(checkout, "changes.txt"), filepath.Join(presets, "saved.yml")} {
				require.Equal(t, before[path], after[path], path)
			}
			cfg.ProjectID = updated.ID
			require.True(t, FeatureLinkValid(cfg, m.Feature))
			repeated, err := InitializeProject(nested, "", "")
			require.NoError(t, err)
			require.Equal(t, updated.ID, repeated.Project.ID)
			require.Zero(t, repeated.Initialized.Tracked)
			require.Equal(t, after, setupFiles(t, base), "repeated init preserves file contents and identities")
		})
	}
}

func TestSetupReadinessChecksMetadata(t *testing.T) {
	for _, tc := range []struct {
		name, code string
		change     func(*testing.T, *models.Config)
	}{
		{"marker", "setup_outdated", func(t *testing.T, cfg *models.Config) {
			st, err := LoadState(cfg.StateFile)
			require.NoError(t, err)
			st.SetupVersion = 0
			require.NoError(t, SaveState(cfg.StateFile, st))
		}},
		{"registration", "registration_missing", func(t *testing.T, cfg *models.Config) {
			require.NoError(t, os.Remove(filepath.Join(cfg.Home, RegistryFileName)))
		}},
		{"actions directory", "directory_missing", func(t *testing.T, cfg *models.Config) {
			require.NoError(t, os.Remove(cfg.ActionsDir))
		}},
		{"state identity", "state_identity_missing", func(t *testing.T, cfg *models.Config) {
			st, err := LoadState(cfg.StateFile)
			require.NoError(t, err)
			st.Project = ""
			require.NoError(t, SaveState(cfg.StateFile, st))
		}},
		{"feature tracking", "feature_state_missing", func(t *testing.T, cfg *models.Config) {
			st, err := LoadState(cfg.StateFile)
			require.NoError(t, err)
			st.Features = nil
			require.NoError(t, SaveState(cfg.StateFile, st))
		}},
		{"feature link", "feature_link_outdated", func(t *testing.T, cfg *models.Config) {
			require.NoError(t, os.Remove(cfg.FeatureLinkPath("demo")))
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(EnvHome, t.TempDir())
			t.Setenv(EnvData, "")
			root, data := t.TempDir(), t.TempDir()
			result, err := InitializeProject(root, data, "demo")
			require.NoError(t, err)
			pd := &models.ProjectDef{ID: result.Project.ID, Name: "demo"}
			cfg, err := Build(root, pd, data)
			require.NoError(t, err)
			require.NoError(t, manifest.Save(cfg.ManifestPath("demo"), &models.Manifest{ID: "feature-id", Project: pd.ID, Feature: "demo"}))
			require.NoError(t, os.MkdirAll(cfg.FeatureDir("demo"), 0o755))
			_, err = InitializeProject(root, data, "")
			require.NoError(t, err)
			tc.change(t, cfg)
			status, err := DetectProject(root, data)
			require.NoError(t, err)
			require.Equal(t, "needs_init", status.Status)
			codes := []string{}
			for _, issue := range status.Issues {
				codes = append(codes, issue.Code)
			}
			require.Contains(t, codes, tc.code)
			result, err = InitializeProject(root, data, "")
			require.NoError(t, err)
			require.Equal(t, "ready", result.Status.Status)
		})
	}
}

func TestSetupBlockedWithoutWrites(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*testing.T, string, string)
	}{
		{"wrong data directory", func(t *testing.T, root, data string) {
			require.NoError(t, SaveState(filepath.Join(data, StateFileName), &models.ProjectState{Project: "different-project"}))
		}},
		{"future setup", func(t *testing.T, root, data string) {
			require.NoError(t, SaveState(filepath.Join(data, StateFileName), &models.ProjectState{SetupVersion: SetupVersion + 1}))
		}},
		{"malformed definition", func(t *testing.T, root, data string) {
			require.NoError(t, os.WriteFile(filepath.Join(root, ProjectDefName), []byte("repos: ["), 0o644))
		}},
		{"foreign manifest", func(t *testing.T, root, data string) {
			require.NoError(t, manifest.Save(filepath.Join(root, WorkwoodDirName, ManifestsDirName, "demo.yaml"), &models.Manifest{Project: "different-project", Feature: "demo"}))
		}},
		{"unsafe feature", func(t *testing.T, root, data string) {
			require.NoError(t, manifest.Save(filepath.Join(root, WorkwoodDirName, ManifestsDirName, "demo.yaml"), &models.Manifest{Feature: "../elsewhere"}))
		}},
		{"duplicate feature identities", func(t *testing.T, root, data string) {
			for _, name := range []string{"first", "second"} {
				require.NoError(t, manifest.Save(filepath.Join(root, WorkwoodDirName, ManifestsDirName, name+".yaml"), &models.Manifest{ID: "same-id", Feature: name}))
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base := t.TempDir()
			root, data := filepath.Join(base, "super"), filepath.Join(base, "data")
			t.Setenv(EnvHome, filepath.Join(base, "home"))
			t.Setenv(EnvData, "")
			_, err := InitializeProject(root, data, "demo")
			require.NoError(t, err)
			tc.change(t, root, data)
			before := setupFiles(t, base)
			status, err := DetectProject(root, data)
			require.Error(t, err)
			require.Equal(t, "blocked", status.Status)
			require.Nil(t, status.InitArguments)
			_, err = InitializeProject(root, data, "")
			require.Error(t, err)
			require.Equal(t, before, setupFiles(t, base))
		})
	}
}

func TestSetupDataDiscovery(t *testing.T) {
	for _, source := range []string{"missing", "argument", "registry", "environment", "settings", "feature_link"} {
		t.Run(source, func(t *testing.T) {
			t.Setenv(EnvHome, t.TempDir())
			t.Setenv(EnvData, "")
			root, data := t.TempDir(), t.TempDir()
			pd := &models.ProjectDef{ID: "project-id", Name: "demo"}
			require.NoError(t, projectdef.Save(filepath.Join(root, ProjectDefName), pd))
			path, argument := root, ""
			switch source {
			case "argument":
				argument = data
				t.Setenv(EnvData, t.TempDir())
			case "registry":
				_, err := RegisterProject(root, data)
				require.NoError(t, err)
				path = pd.ID
				t.Setenv(EnvData, t.TempDir())
			case "environment":
				t.Setenv(EnvData, data)
			case "settings":
				home, err := Home()
				require.NoError(t, err)
				require.NoError(t, SaveApp(home, &models.AppSettings{DataDir: data}))
			case "feature_link":
				cfg, err := Build(root, pd, data)
				require.NoError(t, err)
				require.NoError(t, WriteFeatureLink(cfg, "demo"))
				path, argument = cfg.FeatureDir("demo"), t.TempDir()
			}
			status, err := DetectProject(path, argument)
			require.NoError(t, err)
			if source == "missing" {
				require.Equal(t, "needs_data_dir", status.Status)
				require.Empty(t, status.DataDir)
				_, err := InitializeProject(path, "", "")
				require.Error(t, err)
				require.NoDirExists(t, filepath.Join(root, WorkwoodDirName))
			} else {
				require.Equal(t, data, status.DataDir)
				require.Equal(t, source, status.DataDirSource)
			}
		})
	}
}

func TestSetupWithoutLocalProject(t *testing.T) {
	t.Setenv(EnvHome, t.TempDir())
	t.Setenv(EnvData, "")
	_, err := InitializeProject(t.TempDir(), t.TempDir(), "registered-elsewhere")
	require.NoError(t, err)
	cwd := t.TempDir()
	t.Chdir(cwd)
	status, err := DetectProject("", "")
	require.NoError(t, err)
	require.False(t, status.Found)
	require.Equal(t, "not_found", status.Status)
	_, err = InitializeProject("", t.TempDir(), "")
	require.ErrorContains(t, err, "provide path")
	require.NoFileExists(t, filepath.Join(cwd, ProjectDefName))
}
