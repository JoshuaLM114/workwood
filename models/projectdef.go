package models

// Repo is one base repo entry. URL is the clone source (a full git/gh URL); the
// tool derives nothing — what you put here is what it clones.
type Repo struct {
	Name          string `yaml:"name" json:"name"`                     // local dir name + manifest key
	DefaultBranch string `yaml:"default_branch" json:"default_branch"` // branch to park the base clone on
	URL           string `yaml:"url" json:"url"`                       // required: where to clone from
}

// ProjectDef is the parsed workwood.yml — the committed project definition at the
// super-repo root. ID + Name are the project's shared identity: ID is the UUID
// that links to this developer's external state dir, Name is the immutable
// original_name (the slug used for display defaults; never a branch source).
type ProjectDef struct {
	ID    string `yaml:"id,omitempty" json:"id,omitempty"`     // project UUID (committed; identity)
	Name  string `yaml:"name,omitempty" json:"name,omitempty"` // original_name — canonical label, immutable
	Repos []Repo `yaml:"repos" json:"repos"`
}

// HasIdentity reports whether the project def carries a UUID yet. A legacy or
// freshly hand-written file without one is back-filled by `workwood init`.
func (f *ProjectDef) HasIdentity() bool { return f.ID != "" }

// DefaultBranch returns the configured default branch for repo name, or "" if
// the repo isn't listed.
func (f *ProjectDef) DefaultBranch(name string) string {
	for _, r := range f.Repos {
		if r.Name == name {
			return r.DefaultBranch
		}
	}
	return ""
}

// Names returns the configured repo names in file order.
func (f *ProjectDef) Names() []string {
	out := make([]string, len(f.Repos))
	for i, r := range f.Repos {
		out[i] = r.Name
	}
	return out
}
