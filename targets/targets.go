// Package targets defines a super-feature's per-developer "run targets": where
// each repo should be sourced from when a plugin composes behaviour. These are
// personal choices (not the shared manifest); they are PERSISTED inside the
// developer's workwood-state.yml (see package config), so this package holds only
// the data type + label helpers — no file I/O.
//
// Targets are ADDITIVE and per-repo: a repo carries an ordered LIST of setups,
// and composing emits one context row per setup — so the same repo can take part
// in several ways at once (e.g. a worktree AND a deployed instance).
//
// The built-in setups are:
//   - ignore     — drop this repo from composition (only when it's the SOLE setup)
//   - main       — source from the base reference clone (the repo's default branch)
//   - worktree   — source from one of the feature's local worktrees (code you edit)
//
// A setup may also be ANY OTHER string (e.g. "deploy"). workwood doesn't
// interpret those — it hands the raw value, plus the base clone dir, to the
// repo's .workwood child script, which decides what it means.
//
// A repo with NO setups falls back to the automatic rule (checked-out worktree,
// else main).
package targets

import "strings"

// Source is where a repo is sourced from (a built-in or a user-defined string).
type Source string

const (
	SourceIgnore   Source = "ignore"
	SourceMain     Source = "main"
	SourceWorktree Source = "worktree"
)

// Builtin reports whether s is one of the built-in sources workwood resolves
// itself. Any other non-empty string is a valid user-defined setup too — it's
// handed through to the repo's .workwood child unchanged.
func Builtin(s Source) bool {
	return s == SourceIgnore || s == SourceMain || s == SourceWorktree
}

// Target is one setup in a repo's additive list.
type Target struct {
	Source Source `yaml:"source"`
	// Worktree is the manifest-relative path of the chosen worktree, used when
	// Source is worktree and the repo has more than one. Empty → the primary one.
	Worktree string `yaml:"worktree,omitempty"`
}

// Equal reports whether two setups are the same (so adds can dedupe).
func (t Target) Equal(o Target) bool { return t.Source == o.Source && t.Worktree == o.Worktree }

// Label is a short human label for a setup, with feature stripped from worktree
// paths for brevity.
func (t Target) Label(feature string) string {
	switch t.Source {
	case SourceWorktree:
		if t.Worktree != "" {
			return "wt:" + strings.TrimPrefix(t.Worktree, feature+"/")
		}
		return "worktree"
	default:
		return string(t.Source)
	}
}

// LabelList renders a repo's setups for display ("auto" when empty).
func LabelList(ts []Target, feature string) string {
	if len(ts) == 0 {
		return "auto"
	}
	parts := make([]string, 0, len(ts))
	for _, t := range ts {
		parts = append(parts, t.Label(feature))
	}
	return strings.Join(parts, ", ")
}

// AddTo appends t to a repo's setup list within the given map, skipping an exact
// duplicate. Returns the (possibly new) map and whether it was newly added. Used
// by config.ProjectState to mutate a feature's persisted targets.
func AddTo(m map[string][]Target, repo string, t Target) (map[string][]Target, bool) {
	if m == nil {
		m = map[string][]Target{}
	}
	for _, e := range m[repo] {
		if e.Equal(t) {
			return m, false
		}
	}
	m[repo] = append(m[repo], t)
	return m, true
}

// RemoveFrom drops a matching setup from a repo's list (and the key if it
// empties). Returns whether one was removed.
func RemoveFrom(m map[string][]Target, repo string, t Target) bool {
	list := m[repo]
	for i, e := range list {
		if e.Equal(t) {
			list = append(list[:i], list[i+1:]...)
			if len(list) == 0 {
				delete(m, repo)
			} else {
				m[repo] = list
			}
			return true
		}
	}
	return false
}
