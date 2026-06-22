// Package config resolves the three places workwood keeps state and turns the
// project you're standing in into the absolute paths the rest of the tool uses.
//
// workwood is a GLOBAL CLI with NO project registry. State lives in three places:
//
//   - the SUPER-REPO (committed, shared): workwood.yml at the root holds the
//     project definition + its UUID identity + canonical name; a workwood/ folder
//     holds actions/ (the only action source) and super-features/ (the manifests,
//     each carrying its own UUID + a back-link to the project UUID).
//   - the GLOBAL user dir ~/.workwood (or $WORKWOOD_HOME): app settings ONLY —
//     language, update-check, and a saved fallback for the data-dir path.
//   - the EXTERNAL data dir $WORKWOOD_DATA (never committed): set per project, it
//     points straight at this project's data dir, holding the developer's
//     workwood-state.yml (active names, per-feature targets, optional path
//     overrides) plus the default main/ (base clones) and features/ (worktrees).
//
// A project is located by walking up from the cwd (or an explicit -p <path>) to a
// workwood.yml. The project UUID is stored inside workwood-state.yml as an
// integrity link (it rejects a data dir that belongs to a different project),
// not as a path segment.
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
	WorkwoodDirName  = "workwood"           // committed folder holding actions/ + super-features/
	ActionsDirName   = "actions"            // <root>/workwood/actions
	ManifestsDirName = "super-features"     // <root>/workwood/super-features
	StateFileName    = "workwood-state.yml" // per-developer state, in the data dir
	AppFileName      = "config.yaml"        // ~/.workwood/config.yaml (app settings)

	// RepoWorkwoodDirName is the per-repo folder (in a base clone or worktree) that
	// may hold a targets.yml describing a multi-service repo's sub-paths. The same
	// folder name inside a FEATURE dir holds the back-link file (FeatureLinkName).
	RepoWorkwoodDirName = ".workwood"

	// FeatureLinkName is the back-link file workwood drops in a feature folder
	// (<FeaturesDir>/<slug>/.workwood/link.yml) so the tool can be run from there
	// and resolve back to the parent super-repo.
	FeatureLinkName = "link.yml"

	// FeatureLinkVersion is the back-link file's own format version. It evolves
	// independently of the committed-file version.Schema (the link is a local,
	// regenerable file): bump it when the link layout changes, and EnsureFeatureLink
	// rewrites any out-of-date link the next time the feature is used or relinked.
	FeatureLinkVersion = 1
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
	ActionsDir   string // <Root>/workwood/actions (the only action source)
	ManifestsDir string // <Root>/workwood/super-features
	DataDir      string // $WORKWOOD_DATA (resolved) — this project's data dir
	StateDir     string // = DataDir (no per-project subdir; WORKWOOD_DATA is per-project)
	StateFile    string // <StateDir>/workwood-state.yml
	MainDir      string // base reference clones (state override, else <StateDir>/main)
	FeaturesDir  string // feature worktrees (state override, else <StateDir>/features)

	// ActiveFeature is the feature slug implied by where the command ran: set when
	// the project was resolved via a feature folder's .workwood/link.yml, else "".
	ActiveFeature string
}

// ---- locate errors --------------------------------------------------------

// NotInProjectError means no workwood.yml was found walking up from the start dir.
type NotInProjectError struct{ Start string }

func (e *NotInProjectError) Error() string { return i18n.T("err.not_in_project", e.Start) }

// NoIdentityError means a workwood.yml was found but carries no UUID yet (needs
// `workwood init` to back-fill it).
type NoIdentityError struct{ Root string }

func (e *NoIdentityError) Error() string { return i18n.T("err.no_identity", e.Root) }

// StaleLinkError means a feature folder's link.yml points at a super-repo path
// that no longer holds a workwood.yml (the checkout moved). Re-running `sf up`
// from the super-repo rewrites the link.
type StaleLinkError struct{ Link, SuperRepo string }

func (e *StaleLinkError) Error() string { return i18n.T("err.stale_link", e.Link, e.SuperRepo) }

// ---- feature back-link -----------------------------------------------------

// FeatureLink is the <FeaturesDir>/<slug>/.workwood/link.yml back-link: it records
// (this developer's) super-repo path + data dir so the tool can run from a feature
// folder with neither the super-repo as cwd nor $WORKWOOD_DATA set.
type FeatureLink struct {
	Version   int    `yaml:"version"`    // FeatureLinkVersion it was written with
	SuperRepo string `yaml:"super_repo"` // checkout holding workwood.yml
	DataDir   string `yaml:"data_dir"`   // $WORKWOOD_DATA for this project
	Project   string `yaml:"project"`    // project UUID (integrity)
	Feature   string `yaml:"feature"`    // the feature slug
}

// FeatureLinkPath is the link file's path for a feature slug.
func (c *Config) FeatureLinkPath(slug string) string {
	return filepath.Join(c.FeatureDir(slug), RepoWorkwoodDirName, FeatureLinkName)
}

// WriteFeatureLink writes (or refreshes) the back-link in a feature folder,
// (re)creating the .workwood folder and stamping the current link version.
func WriteFeatureLink(c *Config, slug string) error {
	link := FeatureLink{
		Version:   FeatureLinkVersion,
		SuperRepo: c.Root, DataDir: c.DataDir, Project: c.ProjectID, Feature: slug,
	}
	data, err := yaml.Marshal(link)
	if err != nil {
		return err
	}
	path := c.FeatureLinkPath(slug)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// ReadFeatureLink parses a feature folder's link.yml.
func ReadFeatureLink(path string) (*FeatureLink, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var l FeatureLink
	if err := yaml.Unmarshal(data, &l); err != nil {
		return nil, i18n.Errw(err, "err.parse_file", path)
	}
	return &l, nil
}

// FeatureLinkValid reports whether a feature's back-link is present, the current
// version, and consistent with this config (super-repo, data dir, project). It
// never writes — use EnsureFeatureLink to repair.
func FeatureLinkValid(c *Config, slug string) bool {
	l, err := ReadFeatureLink(c.FeatureLinkPath(slug))
	if err != nil {
		return false
	}
	return version.Normalize(l.Version) == FeatureLinkVersion &&
		l.SuperRepo == c.Root && l.DataDir == c.DataDir &&
		l.Project == c.ProjectID && l.Feature == slug
}

// EnsureFeatureLink (re)writes the back-link only when it's missing, an old
// version, or inconsistent with this config. Returns whether it (re)wrote — so a
// fast "already fine" path stays write-free.
func EnsureFeatureLink(c *Config, slug string) (regenerated bool, err error) {
	if FeatureLinkValid(c, slug) {
		return false, nil
	}
	return true, WriteFeatureLink(c, slug)
}

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

// Location is a resolved project: its super-repo root + parsed def, plus (when the
// command ran inside a feature folder) the data dir and active feature the
// back-link supplied.
type Location struct {
	Root          string
	PD            *projectdef.File
	DataDir       string // from a feature link; "" when resolved via workwood.yml
	ActiveFeature string // feature slug from a feature link; "" otherwise
}

// LocateProject finds the project by walking up from projectFlag (if given) else
// the cwd. At each level it accepts EITHER a workwood.yml (the super-repo root) OR
// a feature folder's .workwood/link.yml (which points back to the super-repo and
// names the active feature) — deepest match wins, so a feature folder resolves to
// its parent. Returns *NotInProjectError / *NoIdentityError / *StaleLinkError when
// preconditions aren't met.
func LocateProject(projectFlag string) (*Location, error) {
	start := projectFlag
	if start == "" {
		cwd, e := os.Getwd()
		if e != nil {
			return nil, e
		}
		start = cwd
	}
	abs, e := filepath.Abs(start)
	if e != nil {
		return nil, e
	}
	root, dataDir, feature, err := findProject(abs)
	if err != nil {
		return nil, err
	}
	if root == "" {
		return nil, &NotInProjectError{Start: abs}
	}
	pd, err := projectdef.Load(filepath.Join(root, ProjectDefName))
	if err != nil {
		return nil, err
	}
	if !pd.HasIdentity() {
		return nil, &NoIdentityError{Root: root}
	}
	return &Location{Root: root, PD: pd, DataDir: dataDir, ActiveFeature: feature}, nil
}

// findProject walks up from dir. It returns the super-repo root for the first
// ancestor that holds a workwood.yml, or — if a feature folder's link.yml is hit
// first — the super-repo + data dir + feature it points to. Returns "" root when
// nothing is found before the filesystem root.
func findProject(dir string) (root, dataDir, feature string, err error) {
	for {
		if _, e := os.Stat(filepath.Join(dir, ProjectDefName)); e == nil {
			return dir, "", "", nil
		}
		linkPath := filepath.Join(dir, RepoWorkwoodDirName, FeatureLinkName)
		if _, e := os.Stat(linkPath); e == nil {
			link, e := ReadFeatureLink(linkPath)
			if e != nil {
				return "", "", "", e
			}
			if _, e := os.Stat(filepath.Join(link.SuperRepo, ProjectDefName)); e != nil {
				return "", "", "", &StaleLinkError{Link: linkPath, SuperRepo: link.SuperRepo}
			}
			return link.SuperRepo, link.DataDir, link.Feature, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", "", "", nil
		}
		dir = parent
	}
}

// Build assembles a Config from a located project and a resolved data dir. The
// data dir IS this project's state dir (WORKWOOD_DATA points straight at it — it
// differs per project), so its workwood-state.yml records the project UUID purely
// as an integrity link: a state file whose stored UUID disagrees with the
// workwood.yml id is rejected (two projects pointed at the same data dir).
func Build(root string, pd *projectdef.File, dataDir string) (*Config, error) {
	home, err := Home()
	if err != nil {
		return nil, err
	}
	stateDir := dataDir
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
	// Base clones + worktrees always live under the data dir — not configurable
	// (WORKWOOD_DATA, set per project, is the single knob).
	mainDir := filepath.Join(stateDir, "main")
	featuresDir := filepath.Join(stateDir, "features")

	return &Config{
		Home:         home,
		Root:         root,
		ProjectDef:   filepath.Join(root, ProjectDefName),
		ProjectID:    pd.ID,
		ProjectSlug:  slug,
		ProjectName:  name,
		ActionsDir:   filepath.Join(root, WorkwoodDirName, ActionsDirName),
		ManifestsDir: filepath.Join(root, WorkwoodDirName, ManifestsDirName),
		DataDir:      dataDir,
		StateDir:     stateDir,
		StateFile:    stateFile,
		MainDir:      mainDir,
		FeaturesDir:  featuresDir,
	}, nil
}

// Resolve is LocateProject + Build in one step (data dir already resolved). The
// interactive data-dir prompt lives in the CLI, so this stays prompt-free. When a
// feature link supplied its own data dir, that wins over the passed-in one.
func Resolve(projectFlag, dataDir string) (*Config, error) {
	loc, err := LocateProject(projectFlag)
	if err != nil {
		return nil, err
	}
	if loc.DataDir != "" {
		dataDir = loc.DataDir
	}
	cfg, err := Build(loc.Root, loc.PD, dataDir)
	if err != nil {
		return nil, err
	}
	cfg.ActiveFeature = loc.ActiveFeature
	return cfg, nil
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
