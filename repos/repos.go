// Package repos manages the base reference clones under a project's main_dir —
// the checkouts that feature worktrees are cut from. They are never edited
// directly.
package repos

import (
	"fmt"
	"os"

	"github.com/JoshuaLM114/workwood/config"
	"github.com/JoshuaLM114/workwood/gitx"
	"github.com/JoshuaLM114/workwood/i18n"
	"github.com/JoshuaLM114/workwood/projectdef"
)

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
