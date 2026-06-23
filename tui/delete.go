package tui

import (
	"strings"

	"github.com/JoshuaLM114/workwood/i18n"
	"github.com/JoshuaLM114/workwood/manifest"
	"github.com/JoshuaLM114/workwood/superfeature"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
)

// deleteRepoVals binds one repo's teardown choices in the delete walkthrough.
// path is the manifest-relative worktree path (what DeleteWalk wants); pathAbs is
// only for display.
type deleteRepoVals struct {
	repo, branch, path, pathAbs string
	removeWorktree              bool
	deleteFiles                 bool
	deleteBranch                bool
}

// deleteVals holds the whole walkthrough: an irreversibility acknowledgement, then
// one set of choices per worktree.
type deleteVals struct {
	confirm bool
	repos   []deleteRepoVals
}

// deleteModel drives the "delete super-feature" walkthrough: a warning + a per-repo
// page asking whether to remove the worktree, delete the local files, and delete
// the branch — then it tears the feature down and shows a log.
type deleteModel struct {
	m    *Model
	slug string
	name string
	form *huh.Form
	vals *deleteVals
	done bool
	log  []string
	err  error
}

// newDeleteModel loads the feature's manifest and builds the walkthrough form. The
// worktree/files toggles default ON (a full delete); the branch toggle defaults OFF
// (branches may be pushed — match `sf delete`, which keeps them without --prune).
func newDeleteModel(m *Model, slug, name string) (*deleteModel, error) {
	man, err := manifest.Load(m.cfg.ManifestPath(slug))
	if err != nil {
		return nil, err
	}
	vals := &deleteVals{}
	for _, w := range man.Worktrees {
		vals.repos = append(vals.repos, deleteRepoVals{
			repo: w.Repo, branch: w.Branch, path: w.Path, pathAbs: m.cfg.Abs(w.Path),
			removeWorktree: true, deleteFiles: true, deleteBranch: false,
		})
	}
	d := &deleteModel{m: m, slug: slug, name: name, vals: vals}
	d.form = newDeleteForm(name, vals).WithWidth(min(80, m.width-4))
	return d, nil
}

// newDeleteForm: page 1 is the irreversible-warning + a Continue/Cancel confirm;
// each later page is one repo with its three teardown toggles (hidden if cancelled).
func newDeleteForm(name string, v *deleteVals) *huh.Form {
	var list strings.Builder
	for _, r := range v.repos {
		list.WriteString(i18n.T("tui.delete.repo_row", r.repo, strings.TrimSpace(r.branch)))
		list.WriteString("\n")
	}
	groups := []*huh.Group{
		huh.NewGroup(
			huh.NewNote().
				Title(i18n.T("tui.delete.warn_title", name)).
				Description(i18n.T("tui.delete.warn_body")+"\n\n"+strings.TrimRight(list.String(), "\n")),
			huh.NewConfirm().
				Key("confirm").
				Title(i18n.T("tui.delete.confirm")).
				Affirmative(i18n.T("tui.delete.continue")).
				Negative(i18n.T("tui.delete.cancel")).
				Value(&v.confirm),
		),
	}
	for i := range v.repos {
		r := &v.repos[i]
		groups = append(groups, huh.NewGroup(
			huh.NewNote().Title(i18n.T("tui.delete.repo_title", r.repo, strings.TrimSpace(r.branch))).Description(r.pathAbs),
			huh.NewConfirm().Title(i18n.T("tui.delete.q_worktree")).Value(&r.removeWorktree),
			huh.NewConfirm().Title(i18n.T("tui.delete.q_files")).Value(&r.deleteFiles),
			huh.NewConfirm().Title(i18n.T("tui.delete.q_branch", strings.TrimSpace(r.branch))).Value(&r.deleteBranch),
		).WithHideFunc(func() bool { return !v.confirm }))
	}
	return form(groups...)
}

func (d *deleteModel) Update(msg tea.Msg) (*deleteModel, tea.Cmd) {
	if k, ok := msg.(tea.KeyMsg); ok {
		switch k.String() {
		case "ctrl+c":
			return d, tea.Quit
		case "esc":
			return d, backToFeatures // cancel (pre-run) or dismiss (post-run)
		case "enter":
			if d.done {
				return d, backToFeatures
			}
		}
	}
	if d.done {
		return d, nil
	}

	fm, cmd := d.form.Update(msg)
	if f, ok := fm.(*huh.Form); ok {
		d.form = f
	}
	switch d.form.State {
	case huh.StateCompleted:
		if !d.vals.confirm { // cancelled on the warning page
			return d, backToFeatures
		}
		plan := make([]superfeature.RepoTeardown, len(d.vals.repos))
		for i, r := range d.vals.repos {
			plan[i] = superfeature.RepoTeardown{
				Repo: r.repo, Branch: r.branch, Path: r.path,
				RemoveWorktree: r.removeWorktree, DeleteFiles: r.deleteFiles, DeleteBranch: r.deleteBranch,
			}
		}
		d.log, d.err = superfeature.DeleteWalk(d.m.cfg, d.slug, plan)
		d.done = true
		return d, nil
	case huh.StateAborted:
		return d, backToFeatures
	}
	return d, cmd
}

func (d *deleteModel) View() string {
	if d.done {
		body := strings.Join(d.log, "\n")
		if d.err != nil {
			body += "\n\n" + errStyle.Render(i18n.T("tui.delete.err", d.err.Error()))
		} else {
			body = okStyle.Render(i18n.T("tui.delete.done", d.name)) + "\n\n" + body
		}
		return docStyle.Render(titleStyle.Render(i18n.T("tui.delete.title", d.name)) + "\n\n" + body + "\n\n" + helpStyle.Render(i18n.T("tui.delete.return")))
	}
	return docStyle.Render(titleStyle.Render(i18n.T("tui.delete.title", d.name)) + "\n\n" + d.form.View() + "\n" + helpStyle.Render(i18n.T("tui.delete.help")))
}

// backToFeatures rebuilds + shows the (now-updated) super-features list.
func backToFeatures() tea.Msg { return openFeaturesMsg{} }
