// Package config resolves the user-local home (~/.workwood) and the registry of
// projects, then turns a selected project into the absolute paths the rest of
// the tool works against.
//
// workwood is a GLOBAL CLI: it is installed once and operates on PROJECTS you
// register. A project is a team "super-repo" checkout holding a workwood.yaml
// (its repo definitions) plus super-features/<name>.yaml manifests — those are
// committed and shared by the team. Your registry entry (in ~/.workwood/
// config.yaml) records where YOU keep the base clones and feature worktrees on
// disk, so the same shared manifests can be rebuilt into any layout per machine.
package config

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/JoshuaLM114/workwood/i18n"
	"github.com/JoshuaLM114/workwood/version"
	"gopkg.in/yaml.v3"
)

// ProjectEntry is one registered project in the user's ~/.workwood/config.yaml.
type ProjectEntry struct {
	Path        string `yaml:"path"`         // super-repo checkout (holds workwood.yaml + super-features/)
	MainDir     string `yaml:"main_dir"`     // where THIS developer keeps base reference clones (<dir>/<repo>)
	FeaturesDir string `yaml:"features_dir"` // where THIS developer keeps feature worktrees (<dir>/<feature>/<...>)
}

// Registry is the parsed ~/.workwood/config.yaml — the user-local project list.
type Registry struct {
	Version        int                     `yaml:"version"`
	Language       string                  `yaml:"language,omitempty"` // UI language (e.g. "ja"); empty → auto-detect
	DefaultProject string                  `yaml:"default_project,omitempty"`
	Projects       map[string]ProjectEntry `yaml:"projects"`
}

// Config holds the resolved, absolute locations for ONE selected project.
type Config struct {
	Home              string // ~/.workwood (or $WORKWOOD_HOME)
	Project           string // selected project name
	Root              string // super-repo checkout (ProjectEntry.Path)
	ProjectDef        string // <Root>/workwood.yaml
	ManifestsDir      string // <Root>/super-features
	ProjectPluginsDir string // <Root>/plugins (project-local plugins, override globals)
	GlobalPluginsDir  string // <Home>/plugins
	MainDir           string // base reference clones
	FeaturesDir       string // feature worktrees
	StateDir          string // <Home>/state/<project> (per-developer run targets)
}

// ProjectDefName is the per-project definition file a super-repo carries.
const ProjectDefName = "workwood.yaml"

// ManifestsDirName is the sub-dir of a super-repo holding super-feature manifests.
const ManifestsDirName = "super-features"

// Home returns the workwood home dir: $WORKWOOD_HOME, else ~/.workwood.
func Home() (string, error) {
	if h := strings.TrimRight(os.Getenv("WORKWOOD_HOME"), "/"); h != "" {
		return h, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".workwood"), nil
}

// RegistryPath is ~/.workwood/config.yaml.
func RegistryPath(home string) string { return filepath.Join(home, "config.yaml") }

// GlobalPluginsDir is ~/.workwood/plugins.
func GlobalPluginsDir(home string) string { return filepath.Join(home, "plugins") }

// LoadRegistry reads ~/.workwood/config.yaml. A missing file yields an empty
// registry (no projects yet), not an error.
func LoadRegistry() (*Registry, string, error) {
	home, err := Home()
	if err != nil {
		return nil, "", err
	}
	reg := &Registry{Projects: map[string]ProjectEntry{}}
	data, err := os.ReadFile(RegistryPath(home))
	if err != nil {
		if os.IsNotExist(err) {
			return reg, home, nil
		}
		return nil, home, err
	}
	if err := yaml.Unmarshal(data, reg); err != nil {
		return nil, home, i18n.Errw(err, "err.parse_file", RegistryPath(home))
	}
	if err := version.CheckSchema(reg.Version, i18n.T("noun.registry")+" "+RegistryPath(home)); err != nil {
		return nil, home, err
	}
	if reg.Projects == nil {
		reg.Projects = map[string]ProjectEntry{}
	}
	return reg, home, nil
}

// SaveRegistry writes ~/.workwood/config.yaml, stamping the current schema.
func SaveRegistry(home string, reg *Registry) error {
	reg.Version = version.Schema
	if reg.Projects == nil {
		reg.Projects = map[string]ProjectEntry{}
	}
	if err := os.MkdirAll(home, 0o755); err != nil {
		return err
	}
	var buf strings.Builder
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(reg); err != nil {
		return err
	}
	enc.Close()
	return os.WriteFile(RegistryPath(home), []byte(buf.String()), 0o644)
}

// Resolve loads the registry, selects a project (explicit flag → cwd autodetect
// → default_project), and builds its absolute Config. It validates the entry but
// does not require the on-disk dirs to exist yet (they're created on demand).
func Resolve(projectFlag string) (*Config, error) {
	reg, home, err := LoadRegistry()
	if err != nil {
		return nil, err
	}
	if len(reg.Projects) == 0 {
		return nil, i18n.Err("err.no_projects")
	}

	name := projectFlag
	if name == "" {
		name = detectProject(reg)
	}
	if name == "" {
		name = reg.DefaultProject
	}
	if name == "" {
		return nil, i18n.Err("err.no_project_selected", strings.Join(projectNames(reg), ", "))
	}
	entry, ok := reg.Projects[name]
	if !ok {
		return nil, i18n.Err("err.unknown_project_reg", name, strings.Join(projectNames(reg), ", "))
	}
	return build(home, name, entry)
}

// build assembles a Config from a registry entry, erroring on missing required
// fields.
func build(home, name string, e ProjectEntry) (*Config, error) {
	root := strings.TrimRight(e.Path, "/")
	mainDir := strings.TrimRight(e.MainDir, "/")
	featuresDir := strings.TrimRight(e.FeaturesDir, "/")
	if root == "" {
		return nil, i18n.Err("err.project_no_path", name)
	}
	if mainDir == "" || featuresDir == "" {
		return nil, i18n.Err("err.project_no_dirs", name, root)
	}
	return &Config{
		Home:              home,
		Project:           name,
		Root:              root,
		ProjectDef:        filepath.Join(root, ProjectDefName),
		ManifestsDir:      filepath.Join(root, ManifestsDirName),
		ProjectPluginsDir: filepath.Join(root, "plugins"),
		GlobalPluginsDir:  GlobalPluginsDir(home),
		MainDir:           mainDir,
		FeaturesDir:       featuresDir,
		StateDir:          filepath.Join(home, "state", name),
	}, nil
}

// detectProject returns the registered project whose path/main_dir/features_dir
// contains the current working directory (longest match wins), or "" if none.
func detectProject(reg *Registry) string {
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}
	cwd = filepath.Clean(cwd)
	best, bestLen := "", -1
	for name, e := range reg.Projects {
		for _, base := range []string{e.Path, e.MainDir, e.FeaturesDir} {
			base = filepath.Clean(strings.TrimRight(base, "/"))
			if base == "" || base == "." {
				continue
			}
			if cwd == base || strings.HasPrefix(cwd, base+string(os.PathSeparator)) {
				if len(base) > bestLen {
					best, bestLen = name, len(base)
				}
			}
		}
	}
	return best
}

func projectNames(reg *Registry) []string {
	names := make([]string, 0, len(reg.Projects))
	for n := range reg.Projects {
		names = append(names, n)
	}
	return names
}

// BaseRepo is the absolute path of a base reference clone for repo.
func (c *Config) BaseRepo(repo string) string { return filepath.Join(c.MainDir, repo) }

// ManifestPath is the absolute path of a super-feature's tracked manifest.
func (c *Config) ManifestPath(feature string) string {
	return filepath.Join(c.ManifestsDir, feature+".yaml")
}

// FeatureDir is the absolute path of a feature's worktree parent dir.
func (c *Config) FeatureDir(feature string) string {
	return filepath.Join(c.FeaturesDir, feature)
}

// StatePath is the absolute path of a feature's per-developer run-target state.
func (c *Config) StatePath(feature string) string {
	return filepath.Join(c.StateDir, feature+".yaml")
}

// Abs turns a manifest-stored (FeaturesDir-relative) worktree path into an
// absolute on-disk path.
func (c *Config) Abs(relPath string) string {
	return filepath.Join(c.FeaturesDir, relPath)
}
