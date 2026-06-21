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
	// A valid action must carry the opt-in marker.
	if err := os.WriteFile(filepath.Join(actionsDir, "noop"), []byte("#!/bin/sh\n# workwood-action: noop\ntrue\n"), 0o755); err != nil {
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

func TestListMarker(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string, mode os.FileMode) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), mode); err != nil {
			t.Fatal(err)
		}
	}
	write("deploy", "#!/bin/sh\n# workwood-action: ship it\ntrue\n", 0o755) // marked + exec → listed
	write("greet", "#!/bin/sh\n#   workwood-action\necho hi\n", 0o755)      // marked, no desc → listed
	write("helper", "#!/bin/sh\necho not an action\n", 0o755)               // exec, no marker → ignored
	write("notexec", "#!/bin/sh\n# workwood-action: x\n", 0o644)            // marked but not exec → ignored
	write("decoy", "#!/bin/sh\n# workwood-actions-helper here\n", 0o755)    // lookalike token → ignored

	cfg := &config.Config{ActionsDir: dir}
	got := List(cfg)
	if len(got) != 2 {
		t.Fatalf("want 2 actions, got %d: %+v", len(got), got)
	}
	// Sorted: deploy, greet.
	if got[0].Name != "deploy" || got[0].Description != "ship it" {
		t.Errorf("deploy: %+v", got[0])
	}
	if got[1].Name != "greet" || got[1].Description != "" {
		t.Errorf("greet: %+v", got[1])
	}
	if Find(cfg, "helper") != "" {
		t.Error("Find should reject an unmarked executable")
	}
	if Find(cfg, "deploy") == "" {
		t.Error("Find should accept a marked action")
	}

	// Scan should report the marked-but-non-executable file (a common mistake).
	_, needChmod := Scan(cfg)
	if len(needChmod) != 1 || needChmod[0] != "notexec" {
		t.Errorf("needChmod = %v, want [notexec]", needChmod)
	}
}

func TestCommandUnknownAction(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{Root: dir, ActionsDir: filepath.Join(dir, "actions"), FeaturesDir: dir}
	if _, err := Command(cfg, "login", "missing", map[string]string{}, nil); err == nil {
		t.Fatal("expected an error for a missing action")
	}
}
