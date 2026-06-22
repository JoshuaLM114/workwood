package action

import (
	"os"
	"os/exec"
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
	// A valid action carries the marker and defines Run + Validate.
	if err := os.WriteFile(filepath.Join(actionsDir, "noop"), []byte("#!/usr/bin/env bash\n# workwood-action: noop\nValidate() { true; }\nRun() { true; }\n"), 0o755); err != nil {
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

	// The per-action state dir is exported and created EMPTY — workwood hands the
	// action a place to write but never writes anything inside it.
	dataDir := cfg.ActionDataDir("login", "noop")
	if !strings.Contains(env, "WORKWOOD_ACTION_DATA="+dataDir) {
		t.Errorf("WORKWOOD_ACTION_DATA not set to the action data dir")
	}
	entries, err := os.ReadDir(dataDir)
	if err != nil {
		t.Fatalf("action data dir should exist: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("workwood must not write into the action data dir, found %d entries", len(entries))
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

func TestMethodsAndValidate(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available")
	}
	dir := t.TempDir()
	actionsDir := filepath.Join(dir, "actions")
	if err := os.MkdirAll(actionsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(actionsDir, name), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// full: both functions; Validate exits 0 (available).
	write("ok", "#!/usr/bin/env bash\n# workwood-action: ok\nValidate() { return 0; }\nRun() { :; }\n")
	// unavailable: Validate exits 1.
	write("bad", "#!/usr/bin/env bash\n# workwood-action: bad\nValidate() { return 1; }\nRun() { :; }\n")
	// partial: missing Validate.
	write("partial", "#!/usr/bin/env bash\n# workwood-action: partial\nRun() { :; }\n")

	cfg := &config.Config{Root: dir, ActionsDir: actionsDir, FeaturesDir: dir}
	by := map[string]Action{}
	for _, a := range List(cfg) {
		by[a.Name] = a
	}
	if a := by["ok"]; !a.HasRun || !a.HasValidate || !a.Runnable() {
		t.Errorf("ok: %+v, want Run+Validate+Runnable", a)
	}
	if a := by["partial"]; !a.HasRun || a.HasValidate || a.Runnable() {
		t.Errorf("partial: %+v, want HasRun only", a)
	}

	if err := Validate(cfg, "f", "ok", nil, nil); err != nil {
		t.Errorf("ok should validate available: %v", err)
	}
	if err := Validate(cfg, "f", "bad", nil, nil); err == nil {
		t.Error("bad should be unavailable (Validate exits 1)")
	}
	// Validating an action with no Validate function errors clearly.
	if err := Validate(cfg, "f", "partial", nil, nil); err == nil {
		t.Error("partial should error (no Validate function)")
	}
}

func TestInit(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available")
	}
	dir := t.TempDir()
	actionsDir := filepath.Join(dir, "actions")
	if err := os.MkdirAll(actionsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// `boot` defines all three: Init creates a marker per target (idempotent),
	// Validate requires it. `plain` has no Init.
	parse := `while IFS= read -r l; do k=${l%%:*}; p=${l#*: }; [ "$k" != "$l" ] || continue; `
	boot := "#!/usr/bin/env bash\n# workwood-action: boot\n" +
		"Validate() { " + parse + `[ -f "$p/.marker" ] || return 1; done < "$WORKWOOD_TARGETS"; }` + "\n" +
		"Run() { :; }\n" +
		"Init() { " + parse + `[ -f "$p/.marker" ] || echo init > "$p/.marker"; done < "$WORKWOOD_TARGETS"; }` + "\n"
	if err := os.WriteFile(filepath.Join(actionsDir, "boot"), []byte(boot), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(actionsDir, "plain"), []byte("#!/usr/bin/env bash\n# workwood-action: x\nRun(){ :; }\nValidate(){ :; }\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{Root: dir, ActionsDir: actionsDir, FeaturesDir: dir}
	tgt := t.TempDir()
	set := map[string]string{"svc": tgt}

	var boota Action
	for _, a := range List(cfg) {
		if a.Name == "boot" {
			boota = a
		}
	}
	if !boota.HasInit || !boota.HasRun || !boota.HasValidate {
		t.Fatalf("boot: %+v, want all three methods", boota)
	}

	if err := Validate(cfg, "f", "boot", set, nil); err == nil {
		t.Error("Validate should fail before Init (no marker)")
	}
	if _, err := Init(cfg, "f", "boot", set, nil); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if _, err := os.Stat(filepath.Join(tgt, ".marker")); err != nil {
		t.Fatalf("Init should create the marker: %v", err)
	}
	if err := Validate(cfg, "f", "boot", set, nil); err != nil {
		t.Errorf("Validate should pass after Init: %v", err)
	}
	if _, err := Init(cfg, "f", "boot", set, nil); err != nil {
		t.Errorf("a second Init should no-op cleanly: %v", err)
	}
	if _, err := Init(cfg, "f", "plain", set, nil); err == nil {
		t.Error("Init on an action with no Init function should error")
	}
}

func TestCommandUnknownAction(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{Root: dir, ActionsDir: filepath.Join(dir, "actions"), FeaturesDir: dir}
	if _, err := Command(cfg, "login", "missing", map[string]string{}, nil); err == nil {
		t.Fatal("expected an error for a missing action")
	}
}
