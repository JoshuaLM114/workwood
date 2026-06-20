package config

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/JoshuaLM114/workwood/i18n"
	"github.com/JoshuaLM114/workwood/targets"
	"github.com/JoshuaLM114/workwood/version"
	"gopkg.in/yaml.v3"
)

// ProjectState is this developer's workwood-state.yml: the single per-project,
// never-committed file under $WORKWOOD_DATA/<project-uuid>/. It holds the editable
// project active_name, optional checkout-path overrides, and one entry per
// super-feature (keyed by the feature's UUID) carrying that feature's editable
// active_name and its personal, additive run-target setups.
type ProjectState struct {
	Version     int                     `yaml:"version"`
	Project     string                  `yaml:"project,omitempty"`      // project UUID (sanity link to workwood.yml)
	Name        string                  `yaml:"name,omitempty"`         // project active_name (default = slug)
	MainDir     string                  `yaml:"main_dir,omitempty"`     // override; default <StateDir>/main
	FeaturesDir string                  `yaml:"features_dir,omitempty"` // override; default <StateDir>/features
	Features    map[string]FeatureState `yaml:"features,omitempty"`     // keyed by feature UUID
}

// FeatureState is one super-feature's local state. Slug caches the immutable
// original_name so a UUID maps back to its manifest filename without a scan.
type FeatureState struct {
	Slug    string                      `yaml:"slug"`
	Name    string                      `yaml:"name,omitempty"`    // active_name (default = slug)
	Targets map[string][]targets.Target `yaml:"targets,omitempty"` // per-repo additive setups
}

// LoadState reads a workwood-state.yml. A missing file yields empty state (not an
// error), so an un-initialised project still resolves to sensible defaults.
func LoadState(path string) (*ProjectState, error) {
	st := &ProjectState{Features: map[string]FeatureState{}}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return st, nil
		}
		return nil, err
	}
	if err := yaml.Unmarshal(data, st); err != nil {
		return nil, i18n.Errw(err, "err.parse_file", path)
	}
	if err := version.CheckSchema(st.Version, i18n.T("noun.state")+" "+path); err != nil {
		return nil, err
	}
	if st.Features == nil {
		st.Features = map[string]FeatureState{}
	}
	return st, nil
}

// SaveState writes a workwood-state.yml, stamping the current schema and creating
// the data dir as needed.
func SaveState(path string, st *ProjectState) error {
	st.Version = version.Schema
	if st.Features == nil {
		st.Features = map[string]FeatureState{}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	var buf strings.Builder
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(st); err != nil {
		return err
	}
	enc.Close()
	return os.WriteFile(path, []byte(buf.String()), 0o644)
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

// TargetsFor returns a feature's ordered setups for a repo (nil if none).
func (s *ProjectState) TargetsFor(uuid, repo string) []targets.Target {
	return s.Features[uuid].Targets[repo]
}

// AddTarget appends a setup to a repo's additive list, skipping exact duplicates.
// Returns whether it was newly added.
func (s *ProjectState) AddTarget(uuid, repo string, t targets.Target) bool {
	f := s.Features[uuid]
	m, added := targets.AddTo(f.Targets, repo, t)
	f.Targets = m
	s.Features[uuid] = f
	return added
}

// RemoveTarget drops a matching setup from a repo's list. Returns whether one
// was removed.
func (s *ProjectState) RemoveTarget(uuid, repo string, t targets.Target) bool {
	f := s.Features[uuid]
	removed := targets.RemoveFrom(f.Targets, repo, t)
	if len(f.Targets) == 0 {
		f.Targets = nil
	}
	s.Features[uuid] = f
	return removed
}

// ClearTarget returns a repo to the automatic default (drops all its setups).
func (s *ProjectState) ClearTarget(uuid, repo string) {
	f := s.Features[uuid]
	delete(f.Targets, repo)
	if len(f.Targets) == 0 {
		f.Targets = nil
	}
	s.Features[uuid] = f
}

// DisplayName returns a feature's active_name, falling back to its slug.
func (f FeatureState) DisplayName() string {
	if f.Name != "" {
		return f.Name
	}
	return f.Slug
}
