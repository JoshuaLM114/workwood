// Package repos manages the base reference clones under a project's main_dir —
// the checkouts that feature worktrees are cut from. They are never edited
// directly.
package repos

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/JoshuaLM114/workwood/i18n"
	"github.com/JoshuaLM114/workwood/libs/gitx"
	"github.com/JoshuaLM114/workwood/models"
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
func Unready(cfg *models.Config, pd *models.ProjectDef) []string {
	var bad []string
	for _, r := range pd.Repos {
		if ClassifyClone(cfg.BaseRepo(r.Name)) != StateClone {
			bad = append(bad, r.Name)
		}
	}
	return bad
}

// SyncInfo is a clone's position relative to its upstream after a fetch.
type SyncInfo struct {
	Ahead, Behind int
	HasUpstream   bool
}

// OutOfSync reports whether origin has commits the local clone doesn't (behind) —
// the case worth warning about, since the developer probably wants to pull.
func (s SyncInfo) OutOfSync() bool { return s.HasUpstream && s.Behind > 0 }

// AheadBehind returns how far a base clone's current branch is from its upstream.
func AheadBehind(dir string) SyncInfo {
	a, b, ok := gitx.AheadBehind(dir)
	return SyncInfo{Ahead: a, Behind: b, HasUpstream: ok}
}

// FetchAll fetches every real clone (quiet, best-effort). It does NOT clone, pull,
// or check anything out — it only refreshes origin refs so sync state is accurate.
func FetchAll(cfg *models.Config, pd *models.ProjectDef) {
	for _, r := range pd.Repos {
		dir := cfg.BaseRepo(r.Name)
		if ClassifyClone(dir) == StateClone {
			_ = gitx.FetchAllQuiet(dir)
		}
	}
}

// OutOfSync returns the names of clones that are behind their upstream (run after
// FetchAll for current numbers).
func OutOfSync(cfg *models.Config, pd *models.ProjectDef) []string {
	var out []string
	for _, r := range pd.Repos {
		dir := cfg.BaseRepo(r.Name)
		if ClassifyClone(dir) == StateClone && AheadBehind(dir).OutOfSync() {
			out = append(out, r.Name)
		}
	}
	return out
}

// ActiveBranch returns the branch currently checked out in a base clone, or "" if
// dir isn't a real clone (or HEAD is detached / unreadable).
func ActiveBranch(dir string) string {
	if ClassifyClone(dir) != StateClone {
		return ""
	}
	b, err := gitx.CurrentBranch(dir)
	if err != nil {
		return ""
	}
	return b
}

// Checkout switches a base clone to branch. git's DWIM creates a local branch
// tracking origin/<branch> when there's no matching local branch.
func Checkout(dir, branch string) error { return gitx.Checkout(dir, branch) }

// BranchRef is a branch available in a base clone, with where it exists. A branch
// can be both local and on origin (e.g. main).
type BranchRef struct {
	Name   string `json:"name"`
	Local  bool   `json:"local"`
	Remote bool   `json:"remote"`
}

// RemoteBranchesFor lists a not-yet-cloned repo's branches straight from its clone
// URL, each marked Remote. Returns nil on any failure (offline, no auth, bad URL)
// so callers fall back to free-text entry.
func RemoteBranchesFor(url string) []BranchRef {
	if url == "" {
		return nil
	}
	names, err := gitx.LsRemoteHeads(url)
	if err != nil {
		return nil
	}
	out := make([]BranchRef, 0, len(names))
	for _, n := range names {
		out = append(out, BranchRef{Name: n, Remote: true})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Branches lists the union of local and origin branches in a base clone, sorted,
// each marked with where it exists. Empty when dir isn't a readable git repo.
func Branches(dir string) []BranchRef {
	byName := map[string]*BranchRef{}
	if locals, err := gitx.LocalBranches(dir); err == nil {
		for _, b := range locals {
			byName[b] = &BranchRef{Name: b, Local: true}
		}
	}
	if remotes, err := gitx.RemoteBranches(dir); err == nil {
		for _, b := range remotes {
			if r, ok := byName[b]; ok {
				r.Remote = true
			} else {
				byName[b] = &BranchRef{Name: b, Remote: true}
			}
		}
	}
	out := make([]BranchRef, 0, len(byName))
	for _, r := range byName {
		out = append(out, *r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Sync clones missing repos and fetches existing ones (parking each on its default
// branch, fast-forwarded). Unlike Pull it captures Git output and returns a log,
// so the TUI can run it without corrupting the terminal. Returns the lines done.
func Sync(cfg *models.Config, pd *models.ProjectDef) ([]string, error) {
	return SyncContext(context.Background(), cfg, pd)
}

// SyncContext clones and fast-forwards source repos, reporting partial progress
// and propagating fetch, checkout and pull failures.
func SyncContext(ctx context.Context, cfg *models.Config, pd *models.ProjectDef) ([]string, error) {
	if err := os.MkdirAll(cfg.MainDir, 0o755); err != nil {
		return nil, err
	}
	var log []string
	for _, r := range pd.Repos {
		dest := cfg.BaseRepo(r.Name)
		switch ClassifyClone(dest) {
		case StateClone:
			if _, err := gitx.RunContext(ctx, dest, "fetch", "--all", "--prune"); err != nil {
				return log, fmt.Errorf("%s: %w", r.Name, err)
			}
			log = append(log, i18n.T("repos.fetched", r.Name))
		case StateMissing:
			if _, err := gitx.RunContext(ctx, "", "clone", "--", r.URL, dest); err != nil {
				return log, fmt.Errorf("%s: %w", r.Name, err)
			}
			log = append(log, i18n.T("repos.cloned", r.Name, r.URL))
		default:
			return log, fmt.Errorf("%s is not a base clone; inspect %s", r.Name, dest)
		}
		if r.DefaultBranch != "" {
			if !gitx.ValidBranchName(dest, r.DefaultBranch) {
				return log, fmt.Errorf("invalid default branch %q for %s", r.DefaultBranch, r.Name)
			}
			if _, err := gitx.RunContext(ctx, dest, "checkout", r.DefaultBranch); err != nil {
				return log, fmt.Errorf("%s: %w", r.Name, err)
			}
		}
		if _, err := gitx.RunContext(ctx, dest, "pull", "--ff-only"); err != nil {
			return log, fmt.Errorf("%s: %w", r.Name, err)
		}
	}
	return log, nil
}

// Pull clones each configured repo into main_dir on first run and fetches it on
// later runs, then parks it on its default branch fast-forwarded to origin.
func Pull(cfg *models.Config, pd *models.ProjectDef) error {
	if err := os.MkdirAll(cfg.MainDir, 0o755); err != nil {
		return err
	}
	for _, r := range pd.Repos {
		dest := cfg.BaseRepo(r.Name)
		switch ClassifyClone(dest) {
		case StateClone:
			fmt.Println(i18n.T("repos.fetching", r.Name))
			if err := gitx.FetchAll(dest); err != nil {
				return fmt.Errorf("%s: %w", r.Name, err)
			}
		case StateMissing:
			fmt.Println(i18n.T("repos.cloning", r.Name, r.URL))
			if err := gitx.Clone(r.URL, dest); err != nil {
				return fmt.Errorf("%s: %w", r.Name, err)
			}
		default:
			return fmt.Errorf("%s is not a base clone; inspect %s", r.Name, dest)
		}
		if r.DefaultBranch != "" {
			if !gitx.ValidBranchName(dest, r.DefaultBranch) {
				return fmt.Errorf("invalid default branch %q for %s", r.DefaultBranch, r.Name)
			}
			if err := gitx.Checkout(dest, r.DefaultBranch); err != nil {
				return fmt.Errorf("%s: %w", r.Name, err)
			}
		}
		if err := gitx.PullFFOnly(dest); err != nil {
			return fmt.Errorf("%s: %w", r.Name, err)
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
func List(cfg *models.Config, pd *models.ProjectDef) []Row {
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
