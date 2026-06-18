// Package projectdef reads (and scaffolds) a project's workwood.yaml — the base
// repos that make up a super-project and each repo's default branch. It is the
// committed, team-shared definition that lives at the root of a super-repo.
package projectdef

import (
	"os"
	"strings"

	"github.com/JoshuaLM114/workwood/i18n"
	"gopkg.in/yaml.v3"
)

// Repo is one base repo entry.
type Repo struct {
	Name          string `yaml:"name"`
	DefaultBranch string `yaml:"default_branch"`
	URL           string `yaml:"url,omitempty"` // optional; overrides <host>/<org>/<name>
}

// File is the parsed workwood.yaml.
type File struct {
	Org   string `yaml:"org"`
	Host  string `yaml:"host,omitempty"` // optional git host (default: github.com via gh)
	Repos []Repo `yaml:"repos"`
}

// Load parses the workwood.yaml at path.
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

// Save writes a workwood.yaml to path (used by `workwood init` when scaffolding a
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

// Slug returns the clone target for a repo: its explicit URL if set, else
// <org>/<name> (which `gh repo clone` resolves against github.com).
func (f *File) Slug(r Repo) string {
	if r.URL != "" {
		return r.URL
	}
	return f.Org + "/" + r.Name
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
