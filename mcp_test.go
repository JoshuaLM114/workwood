package main

import (
	"bufio"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/JoshuaLM114/workwood/config"
)

// TestMCPProcess runs the actual command entry point in an isolated subprocess.
func TestMCPProcess(t *testing.T) {
	if os.Getenv("WORKWOOD_TEST_MCP_PROCESS") != "1" {
		return
	}
	os.Args = []string{"workwood", "mcp"}
	main()
	os.Exit(0)
}

func TestMCPStdio(t *testing.T) {
	home := t.TempDir()
	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestMCPProcess$")
	cmd.Dir = t.TempDir()
	cmd.Env = append(os.Environ(), "WORKWOOD_TEST_MCP_PROCESS=1", "WORKWOOD_HOME="+home, "WORKWOOD_DATA=")
	in, err := cmd.StdinPipe()
	require.NoError(t, err)
	out, err := cmd.StdoutPipe()
	require.NoError(t, err)
	require.NoError(t, cmd.Start())
	t.Cleanup(func() { _ = in.Close(); _ = cmd.Process.Kill(); _ = cmd.Wait() })
	encoder := json.NewEncoder(in)
	scanner := bufio.NewScanner(out)
	scanner.Buffer(make([]byte, 4096), 2<<20)
	for _, tc := range []struct {
		method string
		params any
	}{
		{"initialize", map[string]any{"protocolVersion": "2025-03-26", "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "workwood-test", "version": "1"}}},
		{"tools/list", map[string]any{}},
		{"tools/call", map[string]any{"name": "projects_list", "arguments": map[string]any{}}},
	} {
		require.NoError(t, encoder.Encode(map[string]any{"jsonrpc": "2.0", "id": tc.method, "method": tc.method, "params": tc.params}))
		line := make(chan []byte, 1)
		go func() {
			if scanner.Scan() {
				line <- append([]byte(nil), scanner.Bytes()...)
			} else {
				line <- nil
			}
		}()
		select {
		case raw := <-line:
			require.NotEmpty(t, raw, "%s: %v", tc.method, scanner.Err())
			var response map[string]any
			require.NoError(t, json.Unmarshal(raw, &response), "stdout must contain only JSON-RPC")
			require.Equal(t, "2.0", response["jsonrpc"])
			require.Equal(t, tc.method, response["id"])
			require.NotContains(t, response, "error")
		case <-time.After(10 * time.Second):
			t.Fatal("MCP did not respond")
		}
		if tc.method == "initialize" {
			require.NoError(t, encoder.Encode(map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"}))
		}
	}
	entries, err := os.ReadDir(home)
	require.NoError(t, err)
	require.Empty(t, entries, "MCP discovery must not write config or update-check state")
}

func TestInitAutomaticallyRegistersProject(t *testing.T) {
	t.Setenv(config.EnvHome, t.TempDir())
	root, data := t.TempDir(), t.TempDir()
	t.Setenv(config.EnvData, data)
	require.NoError(t, runInit([]string{root}))
	registered, err := config.ListProjects()
	require.NoError(t, err)
	require.Len(t, registered, 1)
	require.Equal(t, root, registered[0].Root)
	require.Equal(t, data, registered[0].DataDir)
	t.Setenv(config.EnvData, "")
	require.NoError(t, runInit([]string{root}))
	cfg, _, err := loadProject(root)
	require.NoError(t, err)
	require.Equal(t, data, cfg.DataDir)
	require.NoError(t, runProject("", []string{"list"}))
	require.NoError(t, runProject("", []string{"unregister", registered[0].ID}))
	require.FileExists(t, filepath.Join(root, config.ProjectDefName))
	require.NoError(t, runProject("", []string{"register", root, "--data-dir", data}))
	cfg, _, err = loadProject(registered[0].ID)
	require.NoError(t, err)
	require.Equal(t, root, cfg.Root)
}

func TestProjectCheckProcess(t *testing.T) {
	if os.Getenv("WORKWOOD_TEST_CHECK_PROCESS") != "1" {
		return
	}
	for i, arg := range os.Args {
		if arg == "--" {
			os.Args = append([]string{"workwood"}, os.Args[i+1:]...)
			main()
			os.Exit(0)
		}
	}
	t.Fatal("missing command arguments")
}

func TestProjectCheckAndUpgradeFromNestedDirectory(t *testing.T) {
	home, root, data := t.TempDir(), t.TempDir(), t.TempDir()
	t.Setenv(config.EnvHome, home)
	t.Setenv(config.EnvData, "")
	require.NoError(t, os.WriteFile(filepath.Join(root, config.ProjectDefName), []byte("name: existing\nrepos: []\n"), 0o644))
	nested := filepath.Join(root, "nested")
	require.NoError(t, os.MkdirAll(nested, 0o755))
	t.Chdir(nested)
	for _, phase := range []string{"needs_init", "ready"} {
		t.Run(phase, func(t *testing.T) {
			args := []string{"-test.run=^TestProjectCheckProcess$", "--", "project", "check"}
			if phase == "needs_init" {
				args = append(args, "--data-dir", data)
			}
			cmd := exec.CommandContext(t.Context(), os.Args[0], args...)
			cmd.Env = append(os.Environ(), "WORKWOOD_TEST_CHECK_PROCESS=1")
			out, err := cmd.CombinedOutput()
			require.NoError(t, err, string(out))
			var status config.ProjectStatus
			require.NoError(t, json.Unmarshal(out, &status), "check output must contain only the JSON report: %s", out)
			require.Equal(t, phase, status.Status)
			require.Equal(t, root, status.Root)
			require.Equal(t, data, status.DataDir)
			if phase == "needs_init" {
				entries, err := os.ReadDir(home)
				require.NoError(t, err)
				require.Empty(t, entries, "project check must not create registry, config, or update-check state")
				require.NoDirExists(t, filepath.Join(root, config.WorkwoodDirName))
				require.NoFileExists(t, filepath.Join(data, config.StateFileName))
				require.NoError(t, runInit([]string{"--data-dir", data}))
			}
			require.NoFileExists(t, filepath.Join(nested, config.ProjectDefName))
		})
	}
}
