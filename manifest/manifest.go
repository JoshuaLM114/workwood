// Package manifest reads and writes the tracked super-feature manifests.
//
// A manifest (<super-repo>/super-features/<name>.yaml) is the source of truth
// for a super-feature: every repo + branch + worktree path that belongs to it.
// Worktree paths are stored RELATIVE to the developer's features_dir (e.g.
// <feature>/<dir>) so the committed manifest stays portable and a teammate can
// rebuild the exact set into their own layout.
package manifest

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"github.com/JoshuaLM114/workwood/fileio"
	"github.com/JoshuaLM114/workwood/i18n"
	"github.com/JoshuaLM114/workwood/version"
	"gopkg.in/yaml.v3"
)

// Worktree is one repo+branch checkout belonging to a super-feature.
type Worktree struct {
	Repo   string `yaml:"repo"`   // base repo name (dir under main_dir)
	Branch string `yaml:"branch"` // full branch: <feature>/<sub>
	Base   string `yaml:"base"`   // source a NEW branch is cut from
	Path   string `yaml:"path"`   // features_dir-relative worktree path (<feature>/<dir>)
}

// Manifest is a single super-feature. Vars is a free-form, team-shared bag of
// key/values handed to plugins via the context (e.g. a deploy plugin's
// namespace) — workwood itself never interprets them, keeping core unopinionated.
type Manifest struct {
	Version     int               `yaml:"version"`             // on-disk schema version (see version pkg)
	ID          string            `yaml:"id,omitempty"`        // super-feature UUID (committed; identity)
	Project     string            `yaml:"project,omitempty"`   // parent project UUID (links back to workwood.yml id)
	Feature     string            `yaml:"feature"`             // original_name / slug: filename stem
	Shorthand   string            `yaml:"shorthand,omitempty"` // short branch prefix (e.g. mnsf for my-new-super-feature)
	Description string            `yaml:"description"`
	Created     string            `yaml:"created"`
	Vars        map[string]string `yaml:"vars,omitempty"`
	Worktrees   []Worktree        `yaml:"worktrees"`
}

// BranchPrefix is the prefix this feature's worktree branches use: the shorthand,
// or the feature name as a fallback for manifests written before shorthands
// existed. Committing the shorthand lets a teammate map a branch like `mnsf/api`
// back to the super-feature.
func (m *Manifest) BranchPrefix() string {
	if m.Shorthand != "" {
		return m.Shorthand
	}
	return m.Feature
}

// DefaultShorthand derives a branch prefix from a feature name: the first letter
// of each '-' / '_' / space-separated word, lowercased
// (my-new-super-feature → mnsf). A name with no separators yields its first letter.
func DefaultShorthand(name string) string {
	fields := strings.FieldsFunc(name, func(r rune) bool {
		return r == '-' || r == '_' || r == ' '
	})
	var b strings.Builder
	for _, f := range fields {
		rs := []rune(f)
		if len(rs) > 0 {
			b.WriteRune(unicode.ToLower(rs[0]))
		}
	}
	if b.Len() == 0 {
		return name
	}
	return b.String()
}

// Load reads and parses a manifest, normalising worktree paths for back-compat.
func Load(path string) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var m Manifest
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, i18n.Errw(err, "err.parse_file", path)
	}
	if err := version.CheckSchema(m.Version, i18n.T("noun.manifest")+" "+path); err != nil {
		return nil, err
	}
	for i := range m.Worktrees {
		m.Worktrees[i].Path = NormPath(m.Worktrees[i].Path)
	}
	if m.Worktrees == nil {
		m.Worktrees = []Worktree{}
	}
	return &m, nil
}

// Save writes a manifest back to disk, preserving the field order teammates see
// in diffs (feature, description, created, worktrees).
func Save(path string, m *Manifest) error {
	m.Version = version.Schema
	if m.Worktrees == nil {
		m.Worktrees = []Worktree{}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return fileio.WriteYAML(path, m)
}

// NormPath strips a legacy leading "features/" from a manifest-stored worktree
// path so older manifests keep working alongside the current <feature>/<dir> form.
func NormPath(p string) string { return strings.TrimPrefix(p, "features/") }

// Find returns the index of the worktree matching repo+branch, or -1.
func (m *Manifest) Find(repo, branch string) int {
	for i, w := range m.Worktrees {
		if w.Repo == repo && w.Branch == branch {
			return i
		}
	}
	return -1
}

// List loads every manifest under dir, sorted by feature name. A missing
// directory yields an empty slice, not an error.
func List(dir string) ([]*Manifest, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []*Manifest
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		m, err := Load(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Feature < out[j].Feature })
	return out, nil
}
