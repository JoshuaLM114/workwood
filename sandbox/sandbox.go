// Package sandbox scaffolds a fully self-contained playground so a new user can
// explore workwood without touching real GitHub or their real ~/.workwood.
//
// A sandbox is a single directory holding everything: an isolated WORKWOOD_HOME,
// a few sample "base repos" (real local git repos with their own local origins
// and .compose/ components), a team "super-repo" (workwood.yaml + a sample shared
// super-feature + a project-local plugin), a registry already pointing at it, and
// an `activate` script. `source <dir>/activate` and every normal workwood command
// just works — `repos`, `sf add`, `compose`, the TUI — all against local data.
package sandbox

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/JoshuaLM114/workwood/config"
	"github.com/JoshuaLM114/workwood/i18n"
	"github.com/JoshuaLM114/workwood/plugins"
	"github.com/JoshuaLM114/workwood/projectdef"
)

// ProjectName is the registered project name inside a sandbox.
const ProjectName = "sandbox"

// sampleRepo is one fake base repo created in the sandbox.
type sampleRepo struct {
	name          string
	defaultBranch string
	blurb         string
}

var samples = []sampleRepo{
	{"api", "main", "the HTTP API service"},
	{"web", "main", "the web front-end"},
	{"worker", "master", "a background worker (note: default branch is master)"},
}

// Result reports the key paths of a created sandbox.
type Result struct {
	Dir      string
	Home     string
	Activate string
	Project  string
	HasGit   bool
}

// Create scaffolds a new sandbox at dir. The directory must not already exist or
// must be empty. It never touches the user's real ~/.workwood.
func Create(dir string) (*Result, error) {
	if _, err := exec.LookPath("git"); err != nil {
		return nil, i18n.Err("err.sandbox_needs_git")
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	if entries, err := os.ReadDir(abs); err == nil && len(entries) > 0 {
		return nil, i18n.Err("err.sandbox_nonempty", abs)
	}

	home := filepath.Join(abs, "home")
	super := filepath.Join(abs, "super")
	mainDir := filepath.Join(abs, "main")
	featuresDir := filepath.Join(abs, "features")
	binDir := filepath.Join(abs, "bin")
	for _, d := range []string{home, super, mainDir, featuresDir, binDir, filepath.Join(abs, "origins")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return nil, err
		}
	}

	// Sample base repos: each a real git repo with a local origin + .compose/.
	for _, s := range samples {
		if err := createSampleRepo(abs, s); err != nil {
			return nil, i18n.Errw(err, "err.sandbox_sample", s.name)
		}
	}
	_ = os.RemoveAll(filepath.Join(abs, ".seed")) // drop the scratch dir

	// The team super-repo: definition + a shared sample feature + a project plugin.
	pd := &projectdef.File{Org: "workwood-sandbox"}
	for _, s := range samples {
		pd.Repos = append(pd.Repos, projectdef.Repo{Name: s.name, DefaultBranch: s.defaultBranch})
	}
	if err := projectdef.Save(filepath.Join(super, config.ProjectDefName), pd); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Join(super, config.ManifestsDirName), 0o755); err != nil {
		return nil, err
	}
	if err := writeFile(filepath.Join(super, config.ManifestsDirName, "welcome.yaml"), welcomeManifest, 0o644); err != nil {
		return nil, err
	}
	if err := writeFile(filepath.Join(super, "plugins", "context"), contextPlugin, 0o755); err != nil {
		return nil, err
	}

	// Registry written into the SANDBOX home (not the user's real home).
	reg := &config.Registry{
		DefaultProject: ProjectName,
		Projects: map[string]config.ProjectEntry{
			ProjectName: {Path: super, MainDir: mainDir, FeaturesDir: featuresDir},
		},
	}
	if err := config.SaveRegistry(home, reg); err != nil {
		return nil, err
	}
	if _, err := plugins.Seed(config.GlobalPluginsDir(home)); err != nil {
		return nil, err
	}

	// Drop a copy of this binary in so `source activate` makes `workwood` resolve
	// even when it isn't installed on PATH. Best-effort.
	copySelf(filepath.Join(binDir, "workwood"))

	if err := writeFile(filepath.Join(abs, "activate"), activateScript(abs), 0o755); err != nil {
		return nil, err
	}
	if err := writeFile(filepath.Join(abs, "README.md"), walkthrough(abs), 0o644); err != nil {
		return nil, err
	}

	return &Result{Dir: abs, Home: home, Activate: filepath.Join(abs, "activate"), Project: ProjectName, HasGit: true}, nil
}

// createSampleRepo builds origins/<name>.git, seeds a commit with a .compose/
// folder, pushes it, then clones it into main/<name> (so it has a real origin a
// worktree can fetch/push against — a tiny self-contained "GitHub").
func createSampleRepo(abs string, s sampleRepo) error {
	bare := filepath.Join(abs, "origins", s.name+".git")
	if err := git("", "init", "--bare", "-b", s.defaultBranch, bare); err != nil {
		return err
	}
	seed := filepath.Join(abs, ".seed", s.name)
	if err := os.MkdirAll(filepath.Join(seed, ".workwood"), 0o755); err != nil {
		return err
	}
	if err := writeFile(filepath.Join(seed, "README.md"), fmt.Sprintf(sampleRepoReadme, s.name, s.blurb), 0o644); err != nil {
		return err
	}
	for name, body := range map[string]string{
		"up":         upComponent,
		"ssh":        sshComponent,
		"helloworld": helloworldChild,
	} {
		if err := writeFile(filepath.Join(seed, ".workwood", name), body, 0o755); err != nil {
			return err
		}
	}
	for _, args := range [][]string{
		{"init", "-b", s.defaultBranch},
		{"-c", "user.email=sandbox@workwood", "-c", "user.name=workwood sandbox", "add", "-A"},
		{"-c", "user.email=sandbox@workwood", "-c", "user.name=workwood sandbox", "commit", "-m", "initial sandbox commit"},
		{"remote", "add", "origin", bare},
		{"push", "-u", "origin", s.defaultBranch},
	} {
		if err := git(seed, args...); err != nil {
			return err
		}
	}
	if err := git("", "clone", "-q", bare, filepath.Join(abs, "main", s.name)); err != nil {
		return err
	}
	return os.RemoveAll(seed)
}

// git runs git args... in dir (or cwd when dir is "").
func git(dir string, args ...string) error {
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git %s: %w\n%s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

// writeFile writes data to path (creating parent dirs) with the given mode.
func writeFile(path, data string, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(data), mode)
}

// copySelf copies the running executable to dst (best-effort; ignores errors so
// `go run` or odd platforms don't fail the scaffold).
func copySelf(dst string) {
	self, err := os.Executable()
	if err != nil {
		return
	}
	in, err := os.Open(self)
	if err != nil {
		return
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return
	}
	defer out.Close()
	_, _ = io.Copy(out, in)
}

func activateScript(abs string) string {
	return i18n.T("sandbox.activate_tmpl", abs)
}

func walkthrough(abs string) string {
	return i18n.T("sandbox.walkthrough_tmpl", abs)
}
