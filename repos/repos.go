// Package repos manages the base reference clones under a project's main_dir —
// the checkouts that feature worktrees are cut from. They are never edited
// directly.
package repos

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/JoshuaLM114/workwood/config"
	"github.com/JoshuaLM114/workwood/gitx"
	"github.com/JoshuaLM114/workwood/i18n"
	"github.com/JoshuaLM114/workwood/projectdef"
)

// CloneState classifies a base-clone directory under main_dir.
type CloneState int

const (
	StateMissing  CloneState = iota // the directory (or its .git) is absent
	StateNotGit                     // the directory exists but isn't a git repo
	StateWorktree                   // a linked worktree (.git is a file), NOT a real clone
	StateClone                      // a real main clone (.git is a directory)
)

// ClassifyClone reports whether dir is a real main clone. A normal (non-bare)
// clone has a `.git` DIRECTORY; a linked git worktree (or a submodule) has a
// `.git` FILE pointing elsewhere — so this distinguishes "actual clone with the
// main git" from a worktree masquerading as one.
func ClassifyClone(dir string) CloneState {
	info, err := os.Stat(filepath.Join(dir, ".git"))
	if err != nil {
		if _, e := os.Stat(dir); os.IsNotExist(e) {
			return StateMissing
		}
		return StateNotGit
	}
	if info.IsDir() {
		return StateClone
	}
	return StateWorktree
}

// Unready returns the names of configured repos that are NOT a real main clone
// (missing, not a git repo, or a stray worktree) — the repos that must be cloned
// before worktrees can be cut from them. An empty slice means every repo is ready.
func Unready(cfg *config.Config, pd *projectdef.File) []string {
	var bad []string
	for _, r := range pd.Repos {
		if ClassifyClone(cfg.BaseRepo(r.Name)) != StateClone {
			bad = append(bad, r.Name)
		}
	}
	return bad
}

// Sync clones missing repos and fetches existing ones (parking each on its default
// branch, fast-forwarded). Unlike Pull it captures git/gh output and returns a log,
// so the TUI can run it without corrupting the terminal. Returns the lines done.
func Sync(cfg *config.Config, pd *projectdef.File) ([]string, error) {
	if err := os.MkdirAll(cfg.MainDir, 0o755); err != nil {
		return nil, err
	}
	var log []string
	for _, r := range pd.Repos {
		dest := cfg.BaseRepo(r.Name)
		if gitx.IsRepo(dest) {
			if err := gitx.FetchAllQuiet(dest); err != nil {
				return log, err
			}
			log = append(log, i18n.T("repos.fetched", r.Name))
		} else {
			slug := pd.Slug(r)
			if err := gitx.CloneQuiet(slug, dest); err != nil {
				return log, err
			}
			log = append(log, i18n.T("repos.cloned", r.Name, slug))
		}
		if r.DefaultBranch != "" {
			_ = gitx.Checkout(dest, r.DefaultBranch)
			_ = gitx.PullFFOnly(dest)
		}
	}
	return log, nil
}

// Pull clones each configured repo into main_dir on first run and fetches it on
// later runs, then parks it on its default branch fast-forwarded to origin.
func Pull(cfg *config.Config, pd *projectdef.File) error {
	if err := os.MkdirAll(cfg.MainDir, 0o755); err != nil {
		return err
	}
	for _, r := range pd.Repos {
		dest := cfg.BaseRepo(r.Name)
		if gitx.IsRepo(dest) {
			fmt.Println(i18n.T("repos.fetching", r.Name))
			if err := gitx.FetchAll(dest); err != nil {
				return err
			}
		} else {
			slug := pd.Slug(r)
			fmt.Println(i18n.T("repos.cloning", r.Name, slug))
			if err := gitx.Clone(slug, dest); err != nil {
				return err
			}
		}
		if r.DefaultBranch != "" {
			_ = gitx.Checkout(dest, r.DefaultBranch)
			_ = gitx.PullFFOnly(dest)
		}
	}
	fmt.Println(i18n.T("repos.ready", cfg.MainDir))
	return nil
}

// Row is one base repo's listing state.
type Row struct {
	Name          string
	DefaultBranch string
	Cloned        bool
}

// List reports each configured repo with its default branch and clone state.
func List(cfg *config.Config, pd *projectdef.File) []Row {
	rows := make([]Row, 0, len(pd.Repos))
	for _, r := range pd.Repos {
		rows = append(rows, Row{
			Name:          r.Name,
			DefaultBranch: r.DefaultBranch,
			Cloned:        gitx.IsRepo(cfg.BaseRepo(r.Name)),
		})
	}
	return rows
}
