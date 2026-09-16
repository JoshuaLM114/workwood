package mcpserver

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/JoshuaLM114/workwood/config"
	"github.com/JoshuaLM114/workwood/manifest"
	"github.com/JoshuaLM114/workwood/models"
	"github.com/JoshuaLM114/workwood/projectdef"
)

func TestDiscoverAndUpgradeExistingWorktreeProject(t *testing.T) {
	_, c, cfg, _ := fixture(t)
	call(t, c, "feature_create", map[string]any{"name": "login"}, false)
	call(t, c, "feature_add_worktree", map[string]any{"feature": "login", "repo": "api", "branch": "fix"}, false)
	checkout := cfg.Abs("login/api--fix")
	require.NoError(t, os.WriteFile(filepath.Join(checkout, "local.txt"), []byte("unfinished edits\n"), 0o644))
	beforeGit := git(t, checkout, "status", "--porcelain=v1", "--branch")
	call(t, c, "targets_set", map[string]any{"feature": "login", "targets": map[string]string{"custom": checkout}}, false)
	st, err := config.LoadState(cfg.StateFile)
	require.NoError(t, err)
	st.SetupVersion = 0
	require.NoError(t, config.SaveState(cfg.StateFile, st))
	m, err := manifest.Load(cfg.ManifestPath("login"))
	require.NoError(t, err)
	m.Project = ""
	require.NoError(t, manifest.Save(cfg.ManifestPath("login"), m))
	require.NoError(t, config.UnregisterProject(cfg.ProjectID))
	_, c = testClient(t, Options{Project: checkout})

	list := decode[struct {
		Projects []config.RegisteredProject `json:"projects"`
		Detected config.ProjectStatus       `json:"detected_project"`
	}](t, call(t, c, "projects_list", map[string]any{}, false))
	require.Empty(t, list.Projects)
	require.Equal(t, "needs_init", list.Detected.Status)
	require.Equal(t, cfg.Root, list.Detected.Root)
	require.Equal(t, cfg.DataDir, list.Detected.DataDir)
	require.Equal(t, "login", list.Detected.ActiveFeature)
	status := decode[config.ProjectStatus](t, call(t, c, "project_detect", map[string]any{}, false))
	require.Equal(t, "feature_link", status.DataDirSource)
	require.False(t, status.Registered)
	registered, err := config.ListProjects()
	require.NoError(t, err)
	require.Empty(t, registered, "discovery must not register implicitly")

	result := decode[config.ProjectInitialization](t, call(t, c, "project_init", map[string]any{}, false))
	require.Equal(t, "ready", result.Status.Status)
	require.Equal(t, cfg.ProjectID, result.Project.ID)
	require.Equal(t, beforeGit, git(t, checkout, "status", "--porcelain=v1", "--branch"))
	updated, err := config.LoadState(cfg.StateFile)
	require.NoError(t, err)
	require.Equal(t, st.Features, updated.Features)
	require.NoFileExists(t, filepath.Join(checkout, config.ProjectDefName))
	status = decode[config.ProjectStatus](t, call(t, c, "project_detect", map[string]any{"project": cfg.ProjectID}, false))
	require.Equal(t, "ready", status.Status)
	require.Equal(t, "registry", status.DataDirSource)
}

func TestDiscoverPreIdentityProjectAndMissingData(t *testing.T) {
	t.Setenv(config.EnvHome, t.TempDir())
	t.Setenv(config.EnvData, "")
	root, data := t.TempDir(), t.TempDir()
	require.NoError(t, projectdef.Save(filepath.Join(root, config.ProjectDefName), &models.ProjectDef{Name: "existing"}))
	nested := filepath.Join(root, "nested")
	require.NoError(t, os.MkdirAll(nested, 0o755))
	t.Chdir(nested)
	_, c := testClient(t, Options{})
	status := decode[config.ProjectStatus](t, call(t, c, "project_detect", map[string]any{}, false))
	require.Equal(t, "needs_data_dir", status.Status)
	require.Equal(t, root, status.Root)
	call(t, c, "project_init", map[string]any{}, true)
	require.NoDirExists(t, filepath.Join(root, config.WorkwoodDirName))
	status = decode[config.ProjectStatus](t, call(t, c, "project_detect", map[string]any{"data_dir": data}, false))
	require.Equal(t, "needs_init", status.Status)
	result := decode[config.ProjectInitialization](t, call(t, c, "project_init", status.InitArguments, false))
	require.Equal(t, "ready", result.Status.Status)
	require.NotEmpty(t, result.Project.ID)
	require.Equal(t, "existing", result.Project.Name)
	require.NoFileExists(t, filepath.Join(nested, config.ProjectDefName))
}

func TestSetupDiscoveryDoesNotSelectAnUnrelatedProject(t *testing.T) {
	_, c, cfg, _ := fixture(t)
	status := decode[config.ProjectStatus](t, call(t, c, "project_detect", map[string]any{}, false))
	require.Equal(t, "not_found", status.Status)
	call(t, c, "project_init", map[string]any{}, true)
	status = decode[config.ProjectStatus](t, call(t, c, "project_detect", map[string]any{"project": cfg.ProjectID}, false))
	require.Equal(t, "ready", status.Status)
	call(t, c, "project_init", map[string]any{"path": cfg.ProjectID}, false)
	pd, err := projectdef.Load(cfg.ProjectDef)
	require.NoError(t, err)
	pd.ID = "replaced-project"
	require.NoError(t, projectdef.Save(cfg.ProjectDef, pd))
	status = decode[config.ProjectStatus](t, call(t, c, "project_detect", map[string]any{"project": cfg.ProjectID}, true))
	require.Equal(t, "blocked", status.Status)
	require.Nil(t, status.InitArguments)
}
