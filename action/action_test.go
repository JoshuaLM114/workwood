package action

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JoshuaLM114/workwood/config"
)

func TestCommandContextAndEnv(t *testing.T) {
	dir := t.TempDir()
	actionsDir := filepath.Join(dir, "actions")
	if err := os.MkdirAll(actionsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(actionsDir, "noop"), []byte("#!/bin/sh\ntrue\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{Root: dir, ActionsDir: actionsDir, FeaturesDir: dir}

	set := map[string]string{"api": "/abs/api"}
	cmd, err := Command(cfg, "login", "noop", set, map[string]string{"ns": "dev"})
	if err != nil {
		t.Fatal(err)
	}

	// context.yml is written under <FeaturesDir>/<slug>/.
	data, err := os.ReadFile(filepath.Join(dir, "login", "context.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "api: /abs/api") {
		t.Fatalf("context.yml missing target: %s", data)
	}

	env := strings.Join(cmd.Env, "\n")
	for _, want := range []string{"WORKWOOD_FEATURE=login", "WORKWOOD_ACTION=noop", "WORKWOOD_LANG=", "WORKWOOD_VAR_NS=dev"} {
		if !strings.Contains(env, want) {
			t.Errorf("env missing %q", want)
		}
	}
	if !strings.Contains(env, "WORKWOOD_TARGETS="+filepath.Join(dir, "login", "context.yml")) {
		t.Errorf("WORKWOOD_TARGETS not set to context.yml path")
	}
	for _, gone := range []string{"WORKWOOD_PLUGIN", "WORKWOOD_MODE", "WORKWOOD_CONTEXT", "WORKWOOD_SESSION"} {
		if strings.Contains(env, gone+"=") {
			t.Errorf("env should not contain dropped var %q", gone)
		}
	}
}

func TestCommandUnknownAction(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{Root: dir, ActionsDir: filepath.Join(dir, "actions"), FeaturesDir: dir}
	if _, err := Command(cfg, "login", "missing", map[string]string{}, nil); err == nil {
		t.Fatal("expected an error for a missing action")
	}
}
