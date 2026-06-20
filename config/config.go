// Package config resolves the three places workwood keeps state and turns the
// project you're standing in into the absolute paths the rest of the tool uses.
//
// workwood is a GLOBAL CLI with NO project registry. State lives in three places:
//
//   - the SUPER-REPO (committed, shared): workwood.yml at the root holds the
//     project definition + its UUID identity + canonical name; a workwood/ folder
//     holds plugins/ (the only plugin source) and super-features/ (the manifests,
//     each carrying its own UUID + a back-link to the project UUID).
//   - the GLOBAL user dir ~/.workwood (or $WORKWOOD_HOME): app settings ONLY —
//     language, update-check, and a saved fallback for the data-dir path.
//   - the EXTERNAL data dir $WORKWOOD_DATA/<project-uuid>/ (never committed): this
//     developer's workwood-state.yml (active names, per-feature targets, optional
//     path overrides) plus the default main/ (base clones) and features/ (worktrees).
//
// A project is located by walking up from the cwd (or an explicit -p <path>) to a
// workwood.yml; its UUID then names the external state dir. The UUID is the link
// that lets a renamed/re-pathed project still find its own state.
package config

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/JoshuaLM114/workwood/i18n"
	"github.com/JoshuaLM114/workwood/projectdef"
	"github.com/JoshuaLM114/workwood/version"
	"gopkg.in/yaml.v3"
)

// File / dir names.
const (
	ProjectDefName   = "workwood.yml"       // committed project def, at the super-repo root
	WorkwoodDirName  = "workwood"           // committed folder holding plugins/ + super-features/
	PluginsDirName   = "plugins"            // <root>/workwood/plugins
	ManifestsDirName = "super-features"     // <root>/workwood/super-features
	StateFileName    = "workwood-state.yml" // per-developer state, in the data dir
	AppFileName      = "config.yaml"        // ~/.workwood/config.yaml (app settings)
)

// EnvHome and EnvData are the env overrides for the two non-committed locations.
const (
	EnvHome = "WORKWOOD_HOME"
	EnvData = "WORKWOOD_DATA"
)

// AppSettings is the parsed ~/.workwood/config.yaml — global, project-agnostic
// settings only. (Any legacy project-registry fields are ignored on load.)
type AppSettings struct {
	Version     int    `yaml:"version"`
	Language    string `yaml:"language,omitempty"`     // UI language (e.g. "ja"); empty → auto-detect
	UpdateCheck *bool  `yaml:"update_check,omitempty"` // nil → on; false silences the daily update notice
	DataDir     string `yaml:"data_dir,omitempty"`     // saved fallback for $WORKWOOD_DATA
}

// UpdateCheckEnabled reports whether the once-a-day update notice should run.
func (a *AppSettings) UpdateCheckEnabled() bool {
	return a.UpdateCheck == nil || *a.UpdateCheck
}

// Config holds the resolved, absolute locations for the project you're in.
type Config struct {
	Home         string // ~/.workwood (or $WORKWOOD_HOME)
	Root         string // super-repo checkout (holds workwood.yml)
	ProjectDef   string // <Root>/workwood.yml
	ProjectID    string // project UUID (from workwood.yml)
	ProjectSlug  string // original_name (workwood.yml name): branch/display source of truth
	ProjectName  string // active_name (from workwood-state.yml); falls back to slug
	PluginsDir   string // <Root>/workwood/plugins (the only plugin source)
	ManifestsDir string // <Root>/workwood/super-features
	DataDir      string // $WORKWOOD_DATA (resolved)
	StateDir     string // <DataDir>/<project-uuid>
	StateFile    string // <StateDir>/workwood-state.yml
	MainDir      string // base reference clones (state override, else <StateDir>/main)
	FeaturesDir  string // feature worktrees (state override, else <StateDir>/features)
}

// ---- locate errors --------------------------------------------------------

// NotInProjectError means no workwood.yml was found walking up from the start dir.
type NotInProjectError struct{ Start string }

func (e *NotInProjectError) Error() string { return i18n.T("err.not_in_project", e.Start) }

// NoIdentityError means a workwood.yml was found but carries no UUID yet (needs
// `workwood init` to back-fill it).
type NoIdentityError struct{ Root string }

func (e *NoIdentityError) Error() string { return i18n.T("err.no_identity", e.Root) }

// ---- home + app settings --------------------------------------------------

// Home returns the workwood home dir: $WORKWOOD_HOME, else ~/.workwood.
func Home() (string, error) {
	if h := strings.TrimRight(os.Getenv(EnvHome), "/"); h != "" {
		return h, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".workwood"), nil
}

// AppPath is ~/.workwood/config.yaml.
func AppPath(home string) string { return filepath.Join(home, AppFileName) }

// LoadApp reads the global app settings. A missing file yields defaults (not an
// error). Returns the settings and the resolved home dir.
func LoadApp() (*AppSettings, string, error) {
	home, err := Home()
	if err != nil {
		return nil, "", err
	}
	app := &AppSettings{}
	data, err := os.ReadFile(AppPath(home))
	if err != nil {
		if os.IsNotExist(err) {
			return app, home, nil
		}
		return nil, home, err
	}
	if err := yaml.Unmarshal(data, app); err != nil {
		return nil, home, i18n.Errw(err, "err.parse_file", AppPath(home))
	}
	if err := version.CheckSchema(app.Version, i18n.T("noun.app")+" "+AppPath(home)); err != nil {
		return nil, home, err
	}
	return app, home, nil
}

// SaveApp writes the global app settings, stamping the current schema.
func SaveApp(home string, app *AppSettings) error {
	app.Version = version.Schema
	if err := os.MkdirAll(home, 0o755); err != nil {
		return err
	}
	var buf strings.Builder
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(app); err != nil {
		return err
	}
	enc.Close()
	return os.WriteFile(AppPath(home), []byte(buf.String()), 0o644)
}

// ---- project location + build ---------------------------------------------

// LocateProject finds the super-repo: it walks up from projectFlag (if given)
// else the cwd to a workwood.yml, loads it, and requires a UUID. Returns a
// *NotInProjectError or *NoIdentityError when those preconditions aren't met.
func LocateProject(projectFlag string) (root string, pd *projectdef.File, err error) {
	start := projectFlag
	if start == "" {
		cwd, e := os.Getwd()
		if e != nil {
			return "", nil, e
		}
		start = cwd
	}
	abs, e := filepath.Abs(start)
	if e != nil {
		return "", nil, e
	}
	root = findProjectRoot(abs)
	if root == "" {
		return "", nil, &NotInProjectError{Start: abs}
	}
	pd, err = projectdef.Load(filepath.Join(root, ProjectDefName))
	if err != nil {
		return "", nil, err
	}
	if !pd.HasIdentity() {
		return "", nil, &NoIdentityError{Root: root}
	}
	return root, pd, nil
}

// findProjectRoot walks up from dir looking for ProjectDefName, returning the
// containing dir or "" if none is found before the filesystem root.
func findProjectRoot(dir string) string {
	for {
		if _, err := os.Stat(filepath.Join(dir, ProjectDefName)); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// Build assembles a Config from a located project and a resolved data dir. It
// loads the developer's workwood-state.yml (empty if absent) for the active name
// and any path overrides. A state file whose stored project UUID disagrees with
// the workwood.yml id is rejected (a copied/misfiled state dir).
func Build(root string, pd *projectdef.File, dataDir string) (*Config, error) {
	home, err := Home()
	if err != nil {
		return nil, err
	}
	stateDir := filepath.Join(dataDir, pd.ID)
	stateFile := filepath.Join(stateDir, StateFileName)
	st, err := LoadState(stateFile)
	if err != nil {
		return nil, err
	}
	if st.Project != "" && st.Project != pd.ID {
		return nil, i18n.Err("err.project_identity_mismatch", stateFile, st.Project, pd.ID)
	}

	slug := pd.Name
	if slug == "" {
		slug = filepath.Base(root)
	}
	name := st.Name
	if name == "" {
		name = slug
	}
	mainDir := st.MainDir
	if mainDir == "" {
		mainDir = filepath.Join(stateDir, "main")
	}
	featuresDir := st.FeaturesDir
	if featuresDir == "" {
		featuresDir = filepath.Join(stateDir, "features")
	}

	return &Config{
		Home:         home,
		Root:         root,
		ProjectDef:   filepath.Join(root, ProjectDefName),
		ProjectID:    pd.ID,
		ProjectSlug:  slug,
		ProjectName:  name,
		PluginsDir:   filepath.Join(root, WorkwoodDirName, PluginsDirName),
		ManifestsDir: filepath.Join(root, WorkwoodDirName, ManifestsDirName),
		DataDir:      dataDir,
		StateDir:     stateDir,
		StateFile:    stateFile,
		MainDir:      mainDir,
		FeaturesDir:  featuresDir,
	}, nil
}

// Resolve is LocateProject + Build in one step (data dir already resolved). The
// interactive data-dir prompt lives in the CLI, so this stays prompt-free.
func Resolve(projectFlag, dataDir string) (*Config, error) {
	root, pd, err := LocateProject(projectFlag)
	if err != nil {
		return nil, err
	}
	return Build(root, pd, dataDir)
}

// ---- path helpers ---------------------------------------------------------

// BaseRepo is the absolute path of a base reference clone for repo.
func (c *Config) BaseRepo(repo string) string { return filepath.Join(c.MainDir, repo) }

// ManifestPath is the absolute path of a super-feature's tracked manifest
// (keyed by the immutable slug == filename stem).
func (c *Config) ManifestPath(slug string) string {
	return filepath.Join(c.ManifestsDir, slug+".yaml")
}

// FeatureDir is the absolute path of a feature's worktree parent dir.
func (c *Config) FeatureDir(slug string) string {
	return filepath.Join(c.FeaturesDir, slug)
}

// Abs turns a state-stored (FeaturesDir-relative) worktree path into an absolute
// on-disk path.
func (c *Config) Abs(relPath string) string {
	return filepath.Join(c.FeaturesDir, relPath)
}
