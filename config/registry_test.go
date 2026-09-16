package config

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/JoshuaLM114/workwood/models"
	"github.com/JoshuaLM114/workwood/projectdef"
)

func TestRegistryProcess(t *testing.T) {
	if os.Getenv("WORKWOOD_TEST_REGISTRY_PROCESS") != "1" {
		return
	}
	_, err := RegisterProject(os.Getenv("WORKWOOD_TEST_ROOT"), os.Getenv("WORKWOOD_TEST_DATA"))
	require.NoError(t, err)
}

func TestConcurrentRegistryProcesses(t *testing.T) {
	home := t.TempDir()
	t.Setenv(EnvHome, home)
	commands := []*exec.Cmd{}
	for i := 0; i < 5; i++ {
		root := t.TempDir()
		require.NoError(t, projectdef.Save(filepath.Join(root, ProjectDefName), &models.ProjectDef{ID: fmt.Sprintf("project-%d", i), Name: "workwood-test"}))
		cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestRegistryProcess$")
		cmd.Env = append(os.Environ(), "WORKWOOD_TEST_REGISTRY_PROCESS=1", "WORKWOOD_TEST_ROOT="+root, "WORKWOOD_TEST_DATA="+t.TempDir())
		require.NoError(t, cmd.Start())
		commands = append(commands, cmd)
	}
	for _, cmd := range commands {
		require.NoError(t, cmd.Wait())
	}
	projects, err := ListProjects()
	require.NoError(t, err)
	require.Len(t, projects, 5)
}

func TestProjectRegistry(t *testing.T) {
	t.Setenv(EnvHome, t.TempDir())
	projects, err := ListProjects()
	require.NoError(t, err)
	require.Empty(t, projects)
	root, data := t.TempDir(), t.TempDir()
	pd := &models.ProjectDef{ID: "project-one", Name: "workwood-test"}
	require.NoError(t, projectdef.Save(filepath.Join(root, ProjectDefName), pd))
	_, err = InitProject(root, pd, data)
	require.NoError(t, err)
	first, err := RegisterProject(root, data)
	require.NoError(t, err)
	require.Equal(t, root, first.Root)
	require.Equal(t, data, first.DataDir)
	_, err = RegisterProject(root, data)
	require.NoError(t, err)
	projects, err = ListProjects()
	require.NoError(t, err)
	require.Len(t, projects, 1)

	moved := t.TempDir()
	require.NoError(t, projectdef.Save(filepath.Join(moved, ProjectDefName), pd))
	_, err = RegisterProject(moved, data)
	require.NoError(t, err)
	projects, err = ListProjects()
	require.NoError(t, err)
	require.Len(t, projects, 1)
	require.Equal(t, moved, projects[0].Root)
	require.NoError(t, UnregisterProject(pd.ID))
	require.FileExists(t, filepath.Join(moved, ProjectDefName))
	require.DirExists(t, data)
	require.Error(t, UnregisterProject(pd.ID))
}

func TestRegistryRejectsInvalidConfiguration(t *testing.T) {
	for _, tc := range []struct{ name, contents string }{
		{"malformed", "projects: ["},
		{"future", "version: 999\nprojects: []\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv(EnvHome, home)
			require.NoError(t, os.WriteFile(filepath.Join(home, RegistryFileName), []byte(tc.contents), 0o644))
			_, err := ListProjects()
			require.Error(t, err)
			root := t.TempDir()
			require.NoError(t, projectdef.Save(filepath.Join(root, ProjectDefName), &models.ProjectDef{ID: "one", Name: "workwood-test"}))
			_, err = RegisterProject(root, t.TempDir())
			require.Error(t, err)
			data, err := os.ReadFile(filepath.Join(home, RegistryFileName))
			require.NoError(t, err)
			require.Equal(t, tc.contents, string(data))
		})
	}
}

func TestRegisterFeatureLinkAndRejectWrongData(t *testing.T) {
	t.Setenv(EnvHome, t.TempDir())
	root, data := t.TempDir(), t.TempDir()
	pd := &models.ProjectDef{ID: "one", Name: "workwood-test"}
	require.NoError(t, projectdef.Save(filepath.Join(root, ProjectDefName), pd))
	_, err := InitProject(root, pd, data)
	require.NoError(t, err)
	cfg, err := Build(root, pd, data)
	require.NoError(t, err)
	require.NoError(t, WriteFeatureLink(cfg, "feature"))
	p, err := RegisterProject(cfg.FeatureDir("feature"), "")
	require.NoError(t, err)
	require.Equal(t, data, p.DataDir)
	wrong := t.TempDir()
	require.NoError(t, SaveState(filepath.Join(wrong, StateFileName), &models.ProjectState{Project: "another"}))
	_, err = RegisterProject(root, wrong)
	require.Error(t, err)
	_, err = RegisterProject(root, "")
	require.Error(t, err)
	projects, err := ListProjects()
	require.NoError(t, err)
	require.Equal(t, data, projects[0].DataDir)
}
