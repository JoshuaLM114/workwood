// Package targets stores per-developer "run targets" for a super-feature: where
// each repo should be sourced from when a plugin composes behaviour. These are
// personal, machine-local choices (not the shared manifest), so they live under
// ~/.workwood/state/<project>/ rather than in the committed super-repo.
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

import (
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

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

// State is a feature's per-repo setup lists. It has no version of its own: the
// super-feature's version lives in its manifest, which is loaded (and
// version-checked) before state on every path that touches a feature.
type State struct {
	Feature string              `yaml:"feature"`
	Targets map[string][]Target `yaml:"targets"`
}

// Load reads a feature's run-target state. A missing file yields empty state
// (every repo on the automatic default), not an error.
func Load(path, feature string) (*State, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &State{Feature: feature, Targets: map[string][]Target{}}, nil
		}
		return nil, err
	}
	var s State
	if err := yaml.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	if s.Targets == nil {
		s.Targets = map[string][]Target{}
	}
	if s.Feature == "" {
		s.Feature = feature
	}
	return &s, nil
}

// Save writes the state, or removes the file when no setups remain so the
// feature falls fully back to the automatic default.
func Save(path string, s *State) error {
	if len(s.Targets) == 0 {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := yaml.Marshal(s)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// TargetsFor returns the ordered setups for a repo (nil if none).
func (s *State) TargetsFor(repo string) []Target { return s.Targets[repo] }

// Add appends a setup to a repo's list, skipping an exact duplicate. Returns
// false if it was already present.
func (s *State) Add(repo string, t Target) bool {
	if s.Targets == nil {
		s.Targets = map[string][]Target{}
	}
	for _, e := range s.Targets[repo] {
		if e.Equal(t) {
			return false
		}
	}
	s.Targets[repo] = append(s.Targets[repo], t)
	return true
}

// Remove drops a matching setup from a repo's list (and the key if it empties).
// Returns false if no matching setup was found.
func (s *State) Remove(repo string, t Target) bool {
	list := s.Targets[repo]
	for i, e := range list {
		if e.Equal(t) {
			list = append(list[:i], list[i+1:]...)
			if len(list) == 0 {
				delete(s.Targets, repo)
			} else {
				s.Targets[repo] = list
			}
			return true
		}
	}
	return false
}

// Clear removes all of a repo's setups, returning it to the automatic default.
func (s *State) Clear(repo string) { delete(s.Targets, repo) }
