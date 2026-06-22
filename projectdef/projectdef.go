// Package projectdef reads (and scaffolds) a project's workwood.yml — the base
// repos that make up a super-project and each repo's default branch. It is the
// committed, team-shared definition that lives at the root of a super-repo.
package projectdef

import (
	"os"
	"strings"

	"github.com/JoshuaLM114/workwood/i18n"
	"gopkg.in/yaml.v3"
)

// Repo is one base repo entry. URL is the clone source (a full git/gh URL); the
// tool derives nothing — what you put here is what it clones.
type Repo struct {
	Name          string `yaml:"name"`           // local dir name + manifest key
	DefaultBranch string `yaml:"default_branch"` // branch to park the base clone on
	URL           string `yaml:"url"`            // required: where to clone from
}

// File is the parsed workwood.yml — the committed project definition at the
// super-repo root. ID + Name are the project's shared identity: ID is the UUID
// that links to this developer's external state dir, Name is the immutable
// original_name (the slug used for display defaults; never a branch source).
type File struct {
	ID    string `yaml:"id,omitempty"`   // project UUID (committed; identity)
	Name  string `yaml:"name,omitempty"` // original_name — canonical label, immutable
	Repos []Repo `yaml:"repos"`
}

// HasIdentity reports whether the project def carries a UUID yet. A legacy or
// freshly hand-written file without one is back-filled by `workwood init`.
func (f *File) HasIdentity() bool { return f.ID != "" }

// Load parses the workwood.yml at path.
func Load(path string) (*File, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, i18n.Err("err.no_project_def", path)
		}
		return nil, err
	}
	var f File
	if err := yaml.Unmarshal(data, &f); err != nil {
		return nil, i18n.Errw(err, "err.parse_file", path)
	}
	return &f, nil
}

// Save writes a workwood.yml to path (used by `workwood init` when scaffolding a
// fresh super-repo).
func Save(path string, f *File) error {
	var buf strings.Builder
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(f); err != nil {
		return err
	}
	enc.Close()
	return os.WriteFile(path, []byte(buf.String()), 0o644)
}

// DefaultBranch returns the configured default branch for repo name, or "" if
// the repo isn't listed.
func (f *File) DefaultBranch(name string) string {
	for _, r := range f.Repos {
		if r.Name == name {
			return r.DefaultBranch
		}
	}
	return ""
}

// Names returns the configured repo names in file order.
func (f *File) Names() []string {
	out := make([]string, len(f.Repos))
	for i, r := range f.Repos {
		out[i] = r.Name
	}
	return out
}
