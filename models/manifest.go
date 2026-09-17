package models

// Worktree is one repo+branch checkout belonging to a super-feature.
type Worktree struct {
	Repo       string `yaml:"repo" json:"repo"`                                   // base repo name (dir under main_dir)
	Branch     string `yaml:"branch" json:"branch"`                               // full branch: <feature>/<sub>
	Base       string `yaml:"base" json:"base"`                                   // source branch a NEW branch is cut from
	BaseSource string `yaml:"base_source,omitempty" json:"base_source,omitempty"` // origin, local, or pull; empty is legacy auto
	Path       string `yaml:"path" json:"path"`                                   // features_dir-relative worktree path (<feature>/<dir>)
}

// Manifest is a single super-feature. Vars is a free-form, team-shared bag of
// key/values handed to plugins via the context (e.g. a deploy plugin's
// namespace) — workwood itself never interprets them, keeping core unopinionated.
type Manifest struct {
	Version     int               `yaml:"version" json:"version"`                         // on-disk schema version (see version pkg)
	ID          string            `yaml:"id,omitempty" json:"id,omitempty"`               // super-feature UUID (committed; identity)
	Project     string            `yaml:"project,omitempty" json:"project,omitempty"`     // parent project UUID (links back to workwood.yml id)
	Feature     string            `yaml:"feature" json:"feature"`                         // original_name / slug: filename stem
	Shorthand   string            `yaml:"shorthand,omitempty" json:"shorthand,omitempty"` // short branch prefix (e.g. mnsf for my-new-super-feature)
	Description string            `yaml:"description" json:"description"`
	Created     string            `yaml:"created" json:"created"`
	Vars        map[string]string `yaml:"vars,omitempty" json:"vars,omitempty"`
	Worktrees   []Worktree        `yaml:"worktrees" json:"worktrees"`
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

// Find returns the index of the worktree matching repo+branch, or -1.
func (m *Manifest) Find(repo, branch string) int {
	for i, w := range m.Worktrees {
		if w.Repo == repo && w.Branch == branch {
			return i
		}
	}
	return -1
}
