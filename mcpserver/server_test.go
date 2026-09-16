package mcpserver

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/require"

	"github.com/JoshuaLM114/workwood/config"
	"github.com/JoshuaLM114/workwood/manifest"
	"github.com/JoshuaLM114/workwood/models"
	"github.com/JoshuaLM114/workwood/projectdef"
	"github.com/JoshuaLM114/workwood/superfeature"
)

func testClient(t *testing.T, options Options) (*Server, *client.Client) {
	t.Helper()
	s, err := New(options)
	require.NoError(t, err)
	c, err := client.NewInProcessClient(s.MCP)
	require.NoError(t, err)
	require.NoError(t, c.Start(t.Context()))
	t.Cleanup(func() { require.NoError(t, c.Close()) })
	req := mcp.InitializeRequest{}
	req.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	req.Params.ClientInfo = mcp.Implementation{Name: "workwood-test", Version: "1"}
	res, err := c.Initialize(t.Context(), req)
	require.NoError(t, err)
	require.Equal(t, "workwood", res.ServerInfo.Name)
	return s, c
}

func call(t *testing.T, c *client.Client, name string, args any, wantError bool) json.RawMessage {
	t.Helper()
	res, err := c.CallTool(t.Context(), mcp.CallToolRequest{Params: mcp.CallToolParams{Name: name, Arguments: args}})
	require.NoError(t, err, name)
	require.Equal(t, wantError, res.IsError, "%s: %+v", name, res.Content)
	if res.StructuredContent == nil {
		return nil
	}
	data, err := json.Marshal(res.StructuredContent)
	require.NoError(t, err)
	var body struct {
		Data  json.RawMessage `json:"data"`
		Error string          `json:"error"`
	}
	require.NoError(t, json.Unmarshal(data, &body))
	return body.Data
}

func decode[T any](t *testing.T, data []byte) T {
	t.Helper()
	var value T
	require.NoError(t, json.Unmarshal(data, &value))
	return value
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir, "-c", "commit.gpgsign=false", "-c", "core.hooksPath="}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=workwood", "GIT_AUTHOR_EMAIL=test@workwood.test", "GIT_COMMITTER_NAME=workwood", "GIT_COMMITTER_EMAIL=test@workwood.test", "GIT_CONFIG_NOSYSTEM=1")
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %v: %s", args, out)
	return string(out)
}

func fixture(t *testing.T) (*Server, *client.Client, *models.Config, string) {
	t.Helper()
	t.Setenv(config.EnvHome, t.TempDir())
	t.Setenv(config.EnvData, "")
	t.Chdir(t.TempDir())
	s, c := testClient(t, Options{})
	root, data, upstream := t.TempDir(), t.TempDir(), t.TempDir()
	git(t, upstream, "init", "-q", "-b", "main")
	git(t, upstream, "commit", "-q", "--allow-empty", "-m", "initial")
	git(t, upstream, "branch", "existing")
	call(t, c, "project_init", map[string]any{"path": root, "data_dir": data}, false)
	call(t, c, "repo_add", map[string]any{"name": "api", "url": upstream, "default_branch": "main"}, false)
	call(t, c, "repos_pull", map[string]any{}, false)
	pd, err := projectdef.Load(filepath.Join(root, config.ProjectDefName))
	require.NoError(t, err)
	cfg, err := config.Build(root, pd, data)
	require.NoError(t, err)
	return s, c, cfg, upstream
}

func TestCatalogAndInputValidation(t *testing.T) {
	t.Setenv(config.EnvHome, t.TempDir())
	t.Chdir(t.TempDir())
	_, c := testClient(t, Options{})
	list, err := c.ListTools(t.Context(), mcp.ListToolsRequest{})
	require.NoError(t, err)
	require.Len(t, list.Tools, 45)
	byName := map[string]mcp.Tool{}
	for _, tool := range list.Tools {
		require.NotEmpty(t, tool.Description)
		require.NotNil(t, tool.Annotations.ReadOnlyHint)
		require.NotNil(t, tool.Annotations.DestructiveHint)
		byName[tool.Name] = tool
	}
	require.True(t, *byName["projects_list"].Annotations.ReadOnlyHint)
	require.True(t, *byName["project_detect"].Annotations.ReadOnlyHint)
	require.True(t, *byName["action_execute"].Annotations.DestructiveHint)
	require.True(t, *byName["feature_delete"].Annotations.DestructiveHint)
	for _, tc := range []struct {
		name string
		args map[string]any
	}{
		{"project_init", map[string]any{"path": t.TempDir()}},
		{"workwood_version", map[string]any{"unknown": true}},
		{"feature_delete", map[string]any{"feature": "x"}},
		{"feature_folders_apply", map[string]any{"decisions": []any{map[string]any{"worktree": map[string]any{"repo": "api", "branch": "x", "base": "main", "path": "x/api"}, "path": "x/api--x", "missing": false}}}},
	} {
		t.Run(tc.name, func(t *testing.T) { call(t, c, tc.name, tc.args, true) })
	}
	resources, err := c.ListResources(t.Context(), mcp.ListResourcesRequest{})
	require.NoError(t, err)
	require.Len(t, resources.Resources, 1)
	req := mcp.ReadResourceRequest{}
	req.Params.URI = "workwood://guide"
	guide, err := c.ReadResource(t.Context(), req)
	require.NoError(t, err)
	require.NotEmpty(t, guide.Contents)
}

func TestFeatureWorkflow(t *testing.T) {
	_, c, cfg, _ := fixture(t)
	call(t, c, "feature_create", map[string]any{"name": "login", "shorthand": "sf"}, false)
	for _, branch := range []string{"fix-login", "tests"} {
		out := call(t, c, "feature_add_worktree", map[string]any{"feature": "login", "repo": "api", "branch": branch}, false)
		var row struct {
			Worktree models.Worktree `json:"worktree"`
		}
		require.NoError(t, json.Unmarshal(out, &row))
		require.Equal(t, "login/api--"+branch, row.Worktree.Path)
		require.DirExists(t, cfg.Abs(row.Worktree.Path))
	}
	for _, in := range []map[string]any{
		{"feature": "login", "repo": "api", "branch": "fix-login"},
		{"feature": "login", "repo": "api", "branch": "existing", "no_feature_prefix": true},
		{"feature": "login", "repo": "missing", "branch": "new"},
	} {
		call(t, c, "feature_add_worktree", in, true)
	}
	call(t, c, "feature_add_worktree", map[string]any{"feature": "login", "repo": "api", "branch": "existing", "existing_branch": true}, false)
	call(t, c, "feature_update", map[string]any{"feature": "login", "display_name": "Login work", "description": "Test login", "vars": map[string]string{"mode": "test"}}, false)
	call(t, c, "feature_remove_worktree", map[string]any{"feature": "login", "repo": "api", "branch": "sf/tests", "delete_branch": true}, false)
	m, err := manifest.Load(cfg.ManifestPath("login"))
	require.NoError(t, err)
	require.Len(t, m.Worktrees, 2)
	require.NotContains(t, git(t, cfg.BaseRepo("api"), "branch", "--list"), "sf/tests")
	call(t, c, "feature_down", map[string]any{"feature": "login"}, false)
	call(t, c, "feature_up", map[string]any{"feature": "login"}, false)
	require.DirExists(t, cfg.Abs("login/api--fix-login"))
	call(t, c, "feature_delete", map[string]any{"feature": "login", "delete_branches": false}, false)
	require.NoFileExists(t, cfg.ManifestPath("login"))
	require.Contains(t, git(t, cfg.BaseRepo("api"), "branch", "--list"), "sf/fix-login")
}

func TestFoldersAndRepair(t *testing.T) {
	_, c, cfg, _ := fixture(t)
	call(t, c, "feature_create", map[string]any{"name": "legacy"}, false)
	call(t, c, "feature_add_worktree", map[string]any{"feature": "legacy", "repo": "api", "branch": "fix"}, false)
	m, err := manifest.Load(cfg.ManifestPath("legacy"))
	require.NoError(t, err)
	git(t, cfg.BaseRepo("api"), "worktree", "move", cfg.Abs(m.Worktrees[0].Path), cfg.Abs("legacy/api"))
	m.Worktrees[0].Path = "legacy/api"
	require.NoError(t, manifest.Save(cfg.ManifestPath("legacy"), m))
	call(t, c, "feature_up", map[string]any{"feature": "legacy"}, true)
	changes := decode[[]superfeature.FolderChange](t, call(t, c, "feature_folders_check", map[string]any{"feature": "legacy"}, false))
	require.Len(t, changes, 1)
	call(t, c, "feature_folders_apply", map[string]any{"feature": "legacy", "decisions": []superfeature.FolderDecision{{FolderChange: changes[0], Rename: false}}}, false)
	require.DirExists(t, cfg.Abs("legacy/api"))
	m, err = manifest.Load(cfg.ManifestPath("legacy"))
	require.NoError(t, err)
	require.Empty(t, m.Worktrees)
	d := decode[superfeature.DiagnoseResult](t, call(t, c, "feature_diagnose", map[string]any{"feature": "legacy"}, false))
	require.Len(t, d.Orphans, 1)
	call(t, c, "feature_reconcile", map[string]any{"feature": "legacy", "choices": []repairChoice{{Path: "legacy/api", Action: "adopt"}}}, false)
	changes = decode[[]superfeature.FolderChange](t, call(t, c, "feature_folders_check", map[string]any{"feature": "legacy"}, false))
	call(t, c, "feature_folders_apply", map[string]any{"feature": "legacy", "decisions": []superfeature.FolderDecision{{FolderChange: changes[0], Rename: true}}}, false)
	require.DirExists(t, cfg.Abs(changes[0].Path))
	call(t, c, "feature_reconcile", map[string]any{"feature": "legacy", "choices": []repairChoice{{Path: "../../outside", Action: "remove"}}}, true)
	call(t, c, "feature_teardown", map[string]any{"feature": "legacy", "choices": []teardownChoice{{Path: changes[0].Path}}}, false)
	require.DirExists(t, cfg.Abs(changes[0].Path))
	require.NoFileExists(t, cfg.ManifestPath("legacy"))
}

func TestTargetsAndActions(t *testing.T) {
	_, c, cfg, _ := fixture(t)
	call(t, c, "feature_create", map[string]any{"name": "actions"}, false)
	call(t, c, "feature_update", map[string]any{"feature": "actions", "vars": map[string]string{"mode": "test"}}, false)
	script := `# workwood-action: test
Validate() { echo checked; return 7; }
Init() { echo initialized; }
Run() { echo "$WORKWOOD_VAR_MODE"; cat "$WORKWOOD_TARGETS"; cat; echo stderr >&2; }
`
	require.NoError(t, os.WriteFile(filepath.Join(cfg.ActionsDir, "check"), []byte(script), 0o644))
	call(t, c, "actions_list", map[string]any{}, false)
	call(t, c, "action_enable", map[string]any{"action": "check"}, false)
	call(t, c, "targets_set", map[string]any{"feature": "actions", "targets": models.Set{}}, false)
	call(t, c, "preset_save", map[string]any{"feature": "actions", "name": "empty"}, false)
	call(t, c, "targets_set", map[string]any{"feature": "actions", "targets": models.Set{"api": cfg.BaseRepo("api")}}, false)
	call(t, c, "preset_load", map[string]any{"feature": "actions", "name": "empty"}, false)
	current := decode[struct {
		Targets models.Set `json:"targets"`
	}](t, call(t, c, "targets_get", map[string]any{"feature": "actions"}, false))
	require.Empty(t, current.Targets)
	call(t, c, "preset_get", map[string]any{"name": "../outside"}, true)
	call(t, c, "preset_save", map[string]any{"feature": "actions", "name": "empty"}, true)
	for _, tc := range []struct {
		mode, output string
		fail         bool
		exit         int
	}{
		{"run", "test\n{}\ninputstderr\n", false, 0},
		{"validate", "checked\n", true, 7},
		{"init", "initialized\n", false, 0},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			data := call(t, c, "action_execute", map[string]any{"feature": "actions", "action": "check", "mode": tc.mode, "input": "input"}, tc.fail)
			out := decode[struct {
				Output string `json:"output"`
				Exit   int    `json:"exit_code"`
			}](t, data)
			require.Equal(t, tc.output, out.Output)
			require.Equal(t, tc.exit, out.Exit)
		})
	}
	require.NoError(t, os.WriteFile(filepath.Join(cfg.ActionsDir, "slow"), []byte("# workwood-action\nValidate() { :; }\nRun() { echo started; sleep 30; }\n"), 0o755))
	start := time.Now()
	data := call(t, c, "action_execute", map[string]any{"feature": "actions", "action": "slow", "mode": "run", "timeout_seconds": 1}, true)
	require.Less(t, time.Since(start), 5*time.Second)
	require.Contains(t, string(data), "started")
	// A fresh client sees the persisted empty set too.
	_, c2 := testClient(t, Options{})
	current = decode[struct {
		Targets models.Set `json:"targets"`
	}](t, call(t, c2, "targets_get", map[string]any{"feature": "actions"}, false))
	require.Empty(t, current.Targets)
}

func TestProjectSelection(t *testing.T) {
	s, c, cfg, _ := fixture(t)
	first := cfg.ProjectID
	call(t, c, "project_init", map[string]any{"path": t.TempDir(), "data_dir": t.TempDir()}, false)
	call(t, c, "project_info", map[string]any{}, true)
	call(t, c, "project_info", map[string]any{"project": first}, false)
	call(t, c, "project_info", map[string]any{"project": cfg.Root}, false)
	call(t, c, "feature_create", map[string]any{"project": first, "name": "linked"}, false)
	_, c2 := testClient(t, Options{Project: cfg.FeatureDir("linked")})
	call(t, c2, "feature_get", map[string]any{}, false)
	pd, err := projectdef.Load(cfg.ProjectDef)
	require.NoError(t, err)
	pd.ID = "changed"
	require.NoError(t, projectdef.Save(cfg.ProjectDef, pd))
	_, _, err = s.project(ProjectInput{Project: first})
	require.ErrorContains(t, err, "different identity")
}

func TestProjectAndPresetManagement(t *testing.T) {
	_, c, cfg, upstream := fixture(t)
	for _, tc := range []struct {
		name string
		args map[string]any
	}{
		{"workwood_version", map[string]any{}},
		{"projects_list", map[string]any{}},
		{"project_rename", map[string]any{"name": "Local workwood"}},
		{"settings_update", map[string]any{"update_check": false, "data_dir": cfg.DataDir}},
		{"settings_get", map[string]any{}},
		{"repo_branches", map[string]any{"repo": "api"}},
		{"repos_fetch", map[string]any{}},
		{"repo_set_default_branch", map[string]any{"repo": "api", "branch": "existing"}},
		{"repo_set_default_branch", map[string]any{"repo": "api", "branch": "main"}},
		{"repo_add", map[string]any{"name": "extra", "url": upstream, "default_branch": "main"}},
		{"repo_branches", map[string]any{"repo": "extra"}},
		{"repo_remove", map[string]any{"repo": "extra"}},
		{"repos_list", map[string]any{}},
		{"feature_create", map[string]any{"name": "presets"}},
		{"features_list", map[string]any{}},
		{"feature_relink", map[string]any{"feature": "presets"}},
		{"targets_generate", map[string]any{"feature": "presets", "name": "generated"}},
		{"presets_list", map[string]any{}},
		{"preset_get", map[string]any{"name": "generated"}},
		{"preset_delete", map[string]any{"name": "generated"}},
		{"targets_prune", map[string]any{"feature": "presets"}},
		{"targets_expand", map[string]any{"path": cfg.BaseRepo("api")}},
		{"project_unregister", map[string]any{"id": cfg.ProjectID}},
		{"project_register", map[string]any{"path": cfg.Root, "data_dir": cfg.DataDir}},
	} {
		t.Run(tc.name, func(t *testing.T) { call(t, c, tc.name, tc.args, false) })
	}
	st, err := config.LoadState(cfg.StateFile)
	require.NoError(t, err)
	require.Equal(t, "Local workwood", st.Name)
	app, _, err := config.LoadApp()
	require.NoError(t, err)
	require.False(t, app.UpdateCheckEnabled())
	require.NoFileExists(t, filepath.Join(cfg.StateDir, "targets", "generated.yml"))
}

func TestInvalidStartupDoesNotSelectAnotherProject(t *testing.T) {
	_, _, cfg, _ := fixture(t)
	broken := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(broken, config.ProjectDefName), []byte("repos: ["), 0o644))
	t.Chdir(broken)
	_, c := testClient(t, Options{})
	call(t, c, "project_info", map[string]any{}, true)
	call(t, c, "project_info", map[string]any{"project": cfg.ProjectID}, false)
}

func TestCancelledRequestAndOutputLimit(t *testing.T) {
	s, c, cfg, _ := fixture(t)
	call(t, c, "feature_create", map[string]any{"name": "cancel"}, false)
	require.NoError(t, os.WriteFile(filepath.Join(cfg.ActionsDir, "large"), []byte("# workwood-action\nValidate() { :; }\nRun() { head -c 1100000 /dev/zero; }\n"), 0o755))
	data := call(t, c, "action_execute", map[string]any{"feature": "cancel", "action": "large", "mode": "run"}, false)
	out := decode[struct {
		Output    string `json:"output"`
		Truncated bool   `json:"truncated"`
	}](t, data)
	require.Len(t, out.Output, 1<<20)
	require.True(t, out.Truncated)
	s.gate <- struct{}{}
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	res, err := c.CallTool(ctx, mcp.CallToolRequest{Params: mcp.CallToolParams{Name: "workwood_version", Arguments: map[string]any{}}})
	if err == nil {
		require.True(t, res.IsError)
	} else {
		require.ErrorIs(t, err, context.DeadlineExceeded)
	}
	<-s.gate
}
