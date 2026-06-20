package targetcfg

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/JoshuaLM114/workwood/config"
	"github.com/JoshuaLM114/workwood/manifest"
	"github.com/JoshuaLM114/workwood/projectdef"
)

func testCfg(dir string) *config.Config {
	return &config.Config{
		MainDir:     filepath.Join(dir, "main"),
		FeaturesDir: filepath.Join(dir, "features"),
		StateDir:    dir,
		StateFile:   filepath.Join(dir, config.StateFileName),
	}
}

func TestCleanSetAndDedup(t *testing.T) {
	cfg := testCfg(t.TempDir())
	pd := &projectdef.File{Repos: []projectdef.Repo{{Name: "api"}, {Name: "web"}}}
	m := &manifest.Manifest{Feature: "login", Worktrees: []manifest.Worktree{
		{Repo: "api", Branch: "login/api-jwt", Path: "login/api"},
		{Repo: "web", Branch: "login/web", Path: "login/web"}, // sub-branch "web" collides with the web repo
	}}

	set := CleanSet(cfg, pd, m)
	want := map[string]string{
		"api":     cfg.BaseRepo("api"),
		"web":     cfg.BaseRepo("web"),
		"api-jwt": cfg.Abs("login/api"),
		"web-2":   cfg.Abs("login/web"), // deduped
	}
	if len(set) != len(want) {
		t.Fatalf("got %d entries, want %d: %+v", len(set), len(want), set)
	}
	for k, v := range want {
		if set[k] != v {
			t.Errorf("set[%q] = %q, want %q", k, set[k], v)
		}
	}
}

func TestEnableDisableRename(t *testing.T) {
	set := map[string]string{}
	Enable(set, "api", "/a")
	Enable(set, "api", "/a") // same path again → no-op
	if len(set) != 1 {
		t.Fatalf("duplicate path added: %+v", set)
	}
	Enable(set, "api", "/b") // same key, different path → deduped key
	if set["api-2"] != "/b" {
		t.Fatalf("expected api-2 → /b: %+v", set)
	}
	Rename(set, "api", "primary")
	if set["primary"] != "/a" || set["api"] != "" {
		t.Fatalf("rename failed: %+v", set)
	}
	Disable(set, "/a")
	if _, ok := set["primary"]; ok {
		t.Fatalf("disable failed: %+v", set)
	}
}

func TestExpandServices(t *testing.T) {
	root := t.TempDir()
	ww := filepath.Join(root, config.RepoWorkwoodDirName)
	if err := os.MkdirAll(ww, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "api: services/api\nshared: /opt/shared\n"
	if err := os.WriteFile(filepath.Join(ww, "targets.yml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	svc, err := ExpandServices(root)
	if err != nil {
		t.Fatal(err)
	}
	if svc["api"] != filepath.Join(root, "services/api") {
		t.Errorf("relative subpath not joined: %q", svc["api"])
	}
	if svc["shared"] != "/opt/shared" {
		t.Errorf("absolute subpath changed: %q", svc["shared"])
	}

	// Missing file → nil, no error.
	if svc, err := ExpandServices(t.TempDir()); err != nil || svc != nil {
		t.Errorf("missing file: svc=%v err=%v", svc, err)
	}
}

func TestPresetRoundTrip(t *testing.T) {
	cfg := testCfg(t.TempDir())
	in := Set{"api": "/a", "web": "/w"}
	if err := SavePreset(cfg, "mine", in); err != nil {
		t.Fatal(err)
	}
	if got := ListPresets(cfg); len(got) != 1 || got[0] != "mine" {
		t.Fatalf("ListPresets = %v", got)
	}
	out, err := LoadPreset(cfg, "mine")
	if err != nil {
		t.Fatal(err)
	}
	if out["api"] != "/a" || out["web"] != "/w" {
		t.Fatalf("preset round-trip: %+v", out)
	}
}

func TestWorkingRoundTrip(t *testing.T) {
	cfg := testCfg(t.TempDir())
	m := &manifest.Manifest{ID: "feat-uuid", Feature: "login"}
	if err := SaveWorking(cfg, m, Set{"api": "/a"}); err != nil {
		t.Fatal(err)
	}
	got, err := LoadWorking(cfg, m)
	if err != nil {
		t.Fatal(err)
	}
	if got["api"] != "/a" {
		t.Fatalf("working round-trip: %+v", got)
	}
}

func TestAddPathGitBranch(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q", "-b", "feature/login"},
		{"-c", "user.email=t@t", "-c", "user.name=t", "commit", "-q", "--allow-empty", "-m", "x"},
	} {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Skipf("git setup failed: %v\n%s", err, out)
		}
	}
	key, abs, needsKey := AddPath(dir)
	if needsKey {
		t.Fatalf("git path should not need a key")
	}
	if key != "feature/login" {
		t.Errorf("default key = %q, want feature/login", key)
	}
	if abs != dir {
		t.Errorf("abs = %q, want %q", abs, dir)
	}

	// A non-git dir needs a key.
	if _, _, needsKey := AddPath(t.TempDir()); !needsKey {
		t.Errorf("non-git path should need a key")
	}
}
