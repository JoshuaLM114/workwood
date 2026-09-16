package models

import "path/filepath"

// AppSettings is the parsed ~/.workwood/config.yaml — global, project-agnostic
// settings only. The project registry lives separately in projects.yml.
type AppSettings struct {
	Version     int    `yaml:"version" json:"version"`
	Language    string `yaml:"language,omitempty" json:"language,omitempty"`         // UI language (e.g. "ja"); empty → auto-detect
	UpdateCheck *bool  `yaml:"update_check,omitempty" json:"update_check,omitempty"` // nil → on; false silences the daily update notice
	DataDir     string `yaml:"data_dir,omitempty" json:"data_dir,omitempty"`         // saved fallback for $WORKWOOD_DATA
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

// FeatureLinkPath is the link file's path for a feature slug.
func (c *Config) FeatureLinkPath(slug string) string {
	return filepath.Join(c.FeatureDir(slug), RepoWorkwoodDirName, FeatureLinkName)
}

// ActionDataDir is the private state directory workwood hands an action for a
// feature: <FeatureDir>/.workwood/action-data/<action>/. workwood creates the
// (empty) dir and passes its path via WORKWOOD_ACTION_DATA, but never reads or
// writes anything inside — the action owns its own state files.
func (c *Config) ActionDataDir(slug, action string) string {
	return filepath.Join(c.FeatureDir(slug), RepoWorkwoodDirName, ActionDataDirName, action)
}

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

// FeatureLink is the <FeaturesDir>/<slug>/.workwood/link.yml back-link: it records
// (this developer's) super-repo path + data dir so the tool can run from a feature
// folder with neither the super-repo as cwd nor $WORKWOOD_DATA set.
type FeatureLink struct {
	Version   int    `yaml:"version" json:"version"`       // FeatureLinkVersion it was written with
	SuperRepo string `yaml:"super_repo" json:"super_repo"` // checkout holding workwood.yml
	DataDir   string `yaml:"data_dir" json:"data_dir"`     // $WORKWOOD_DATA for this project
	Project   string `yaml:"project" json:"project"`       // project UUID (integrity)
	Feature   string `yaml:"feature" json:"feature"`       // the feature slug
}

// ProjectState is this developer's workwood-state.yml: the single per-project,
// never-committed file at $WORKWOOD_DATA/workwood-state.yml. It holds the editable
// project active_name, a project-UUID integrity link, and one entry per
// super-feature (keyed by the feature's UUID) carrying that feature's editable
// active_name and its target working set. Checkout paths are not stored here —
// they're always derived from WORKWOOD_DATA.
type ProjectState struct {
	Version      int                     `yaml:"version" json:"version"`
	SetupVersion int                     `yaml:"setup_version,omitempty" json:"setup_version,omitempty"` // completed local initialization generation
	Project      string                  `yaml:"project,omitempty" json:"project,omitempty"`             // project UUID (sanity link to workwood.yml)
	Name         string                  `yaml:"name,omitempty" json:"name,omitempty"`                   // project active_name (default = slug)
	Features     map[string]FeatureState `yaml:"features,omitempty" json:"features,omitempty"`           // keyed by feature UUID
}

// FeatureState is one super-feature's local state. Slug caches the immutable
// original_name so a UUID maps back to its manifest filename without a scan.
// Targets is this developer's "working set": the enabled targets for the feature,
// each an editable key → absolute path. (The yaml key is `working_set`, distinct
// from any earlier `targets:` shape, so old files migrate by simply being ignored.)
type FeatureState struct {
	Slug                 string            `yaml:"slug" json:"slug"`
	Name                 string            `yaml:"name,omitempty" json:"name,omitempty"` // active_name (default = slug)
	Targets              map[string]string `yaml:"working_set,omitempty" json:"working_set,omitempty"`
	WorkingSetConfigured bool              `yaml:"working_set_configured,omitempty" json:"working_set_configured,omitempty"`
	LastPreset           string            `yaml:"last_preset,omitempty" json:"last_preset,omitempty"` // last targets preset loaded/saved (UI memory)
}

// FeatureByUUID returns a feature's state and whether it exists.
func (s *ProjectState) FeatureByUUID(uuid string) (FeatureState, bool) {
	f, ok := s.Features[uuid]
	return f, ok
}

// FeatureBySlug finds a feature entry (and its UUID) by slug, or ok=false.
func (s *ProjectState) FeatureBySlug(slug string) (uuid string, fs FeatureState, ok bool) {
	for id, f := range s.Features {
		if f.Slug == slug {
			return id, f, true
		}
	}
	return "", FeatureState{}, false
}

// EnsureFeature records a feature (active_name defaulting to slug) if its UUID
// isn't tracked yet. Returns true when it added a new entry (idempotent).
func (s *ProjectState) EnsureFeature(uuid, slug string) bool {
	if s.Features == nil {
		s.Features = map[string]FeatureState{}
	}
	if _, ok := s.Features[uuid]; ok {
		return false
	}
	s.Features[uuid] = FeatureState{Slug: slug, Name: slug}
	return true
}

// WorkingSet returns a feature's working set (key → absolute path), or nil.
func (s *ProjectState) WorkingSet(uuid string) map[string]string {
	return s.Features[uuid].Targets
}

// SetWorkingSet replaces a feature's working set, preserving an explicit empty selection.
func (s *ProjectState) SetWorkingSet(uuid string, set map[string]string) {
	f := s.Features[uuid]
	if len(set) == 0 {
		set = nil
	}
	f.Targets = set
	f.WorkingSetConfigured = true
	s.Features[uuid] = f
}

// LastPreset returns the targets preset last loaded/saved for a feature, or "".
func (s *ProjectState) LastPreset(uuid string) string {
	return s.Features[uuid].LastPreset
}

// SetLastPreset records the targets preset last loaded/saved for a feature.
func (s *ProjectState) SetLastPreset(uuid, name string) {
	f := s.Features[uuid]
	f.LastPreset = name
	s.Features[uuid] = f
}

// DisplayName returns a feature's active_name, falling back to its slug.
func (f FeatureState) DisplayName() string {
	if f.Name != "" {
		return f.Name
	}
	return f.Slug
}
