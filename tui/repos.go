package tui

import (
	"fmt"
	"strings"

	"github.com/JoshuaLM114/workwood/i18n"
	"github.com/JoshuaLM114/workwood/projectdef"
	"github.com/JoshuaLM114/workwood/repos"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
)

type reposFormMode int

const (
	rfNone reposFormMode = iota
	rfAdd
	rfEditBranch
)

type reposDoneMsg struct {
	log []string
	err error
}

// repoRow is one base repo plus its on-disk clone state and active branch.
type repoRow struct {
	name   string
	branch string // configured default branch (workwood.yml)
	active string // branch actually checked out in the base clone ("" if N/A)
	state  repos.CloneState
}

// diverged reports whether the clone is on a different branch than configured.
func (row repoRow) diverged() bool {
	return row.state == repos.StateClone && row.active != "" && row.active != row.branch
}

// reposModel is the "Edit project" screen: add/remove the base repos in the
// committed workwood.yml, see which are actually cloned (real clones — not
// worktrees) under main_dir, and fetch+pull them all. Edits write workwood.yml
// immediately (it's just the project definition — clones/worktrees are untouched).
type reposModel struct {
	m          *Model
	rows       []repoRow
	cursor     int
	form       *huh.Form
	formMode   reposFormMode
	repoVals   repoVals
	branchVals branchVals
	status     string
	busy       bool
	width      int
	height     int
}

func newReposModel(m *Model) *reposModel {
	r := &reposModel{m: m}
	r.rebuild()
	return r
}

func (r *reposModel) rebuild() {
	r.rows = r.rows[:0]
	for _, repo := range r.m.pd.Repos {
		base := r.m.cfg.BaseRepo(repo.Name)
		r.rows = append(r.rows, repoRow{
			name:   repo.Name,
			branch: repo.DefaultBranch,
			active: repos.ActiveBranch(base),
			state:  repos.ClassifyClone(base),
		})
	}
	if r.cursor >= len(r.rows) {
		r.cursor = len(r.rows) - 1
	}
	if r.cursor < 0 {
		r.cursor = 0
	}
}

func (r *reposModel) setSize(w, h int) { r.width, r.height = w, h }

func (r *reposModel) Update(msg tea.Msg) (*reposModel, tea.Cmd) {
	if r.form != nil {
		if k, ok := msg.(tea.KeyMsg); ok {
			switch k.String() {
			case "esc":
				r.form = nil
				r.formMode = rfNone
				r.status = i18n.T("tui.status.cancelled")
				return r, nil
			case "ctrl+c":
				return r, tea.Quit
			}
		}
		fm, cmd := r.form.Update(msg)
		if f, ok := fm.(*huh.Form); ok {
			r.form = f
		}
		if r.form.State == huh.StateCompleted {
			r.onFormDone()
		} else if r.form.State == huh.StateAborted {
			r.form = nil
			r.formMode = rfNone
		}
		return r, cmd
	}

	switch msg := msg.(type) {
	case reposDoneMsg:
		r.busy = false
		r.rebuild()
		if msg.err != nil {
			r.status = errStyle.Render(i18n.T("tui.repos.sync_failed", msg.err.Error()))
		} else {
			r.status = okStyle.Render(i18n.T("tui.repos.synced", len(msg.log)))
		}
		return r, nil

	case tea.KeyMsg:
		if r.busy {
			if msg.String() == "ctrl+c" {
				return r, tea.Quit
			}
			return r, nil
		}
		switch msg.String() {
		case "ctrl+c":
			return r, tea.Quit
		case "esc":
			return r, func() tea.Msg { return backMsg{} }
		case "up", "k":
			if r.cursor > 0 {
				r.cursor--
			}
		case "down", "j":
			if r.cursor < len(r.rows)-1 {
				r.cursor++
			}
		case "a":
			r.repoVals = repoVals{branch: "main"}
			r.form = newAddRepoForm(&r.repoVals).WithWidth(min(72, r.width-2))
			r.formMode = rfAdd
			return r, r.form.Init()
		case "e":
			if r.cursor >= 0 && r.cursor < len(r.m.pd.Repos) {
				r.branchVals = branchVals{branch: r.m.pd.Repos[r.cursor].DefaultBranch}
				r.form = newEditBranchForm(&r.branchVals).WithWidth(min(72, r.width-2))
				r.formMode = rfEditBranch
				return r, r.form.Init()
			}
			return r, nil
		case "d", "x":
			r.removeSelected()
			return r, nil
		case "p":
			r.busy = true
			r.status = i18n.T("tui.repos.syncing")
			return r, r.syncCmd()
		}
	}
	return r, nil
}

func (r *reposModel) syncCmd() tea.Cmd {
	cfg, pd := r.m.cfg, r.m.pd
	return func() tea.Msg {
		log, err := repos.Sync(cfg, pd)
		return reposDoneMsg{log: log, err: err}
	}
}

func (r *reposModel) onFormDone() {
	switch r.formMode {
	case rfAdd:
		r.addRepo()
	case rfEditBranch:
		r.editBranch()
	}
	r.form = nil
	r.formMode = rfNone
}

func (r *reposModel) addRepo() {
	name := strings.TrimSpace(r.repoVals.name)
	if name == "" {
		return
	}
	for _, e := range r.m.pd.Repos {
		if e.Name == name {
			r.status = errStyle.Render(i18n.T("tui.repos.exists", name))
			return
		}
	}
	branch := strings.TrimSpace(r.repoVals.branch)
	if branch == "" {
		branch = "main"
	}
	r.m.pd.Repos = append(r.m.pd.Repos, projectdef.Repo{
		Name: name, DefaultBranch: branch, URL: strings.TrimSpace(r.repoVals.url),
	})
	r.save()
	r.rebuild()
	r.status = okStyle.Render(i18n.T("tui.repos.added", name))
}

// editBranch sets a repo's default branch in workwood.yml, then checks the base
// clone out to it. A checkout failure (missing branch, dirty tree, …) is reported
// but the config change stands — the Active column then shows the divergence.
func (r *reposModel) editBranch() {
	if r.cursor < 0 || r.cursor >= len(r.m.pd.Repos) {
		return
	}
	branch := strings.TrimSpace(r.branchVals.branch)
	if branch == "" {
		return
	}
	repo := &r.m.pd.Repos[r.cursor]
	repo.DefaultBranch = branch
	r.save()

	base := r.m.cfg.BaseRepo(repo.Name)
	if repos.ClassifyClone(base) != repos.StateClone {
		r.rebuild()
		r.status = okStyle.Render(i18n.T("tui.repos.default_set", repo.Name, branch))
		return
	}
	if err := repos.Checkout(base, branch); err != nil {
		r.rebuild()
		r.status = errStyle.Render(i18n.T("tui.repos.checkout_failed", branch, err.Error()))
		return
	}
	r.rebuild()
	r.status = okStyle.Render(i18n.T("tui.repos.checked_out", repo.Name, branch))
}

func (r *reposModel) removeSelected() {
	if r.cursor < 0 || r.cursor >= len(r.m.pd.Repos) {
		return
	}
	name := r.m.pd.Repos[r.cursor].Name
	r.m.pd.Repos = append(r.m.pd.Repos[:r.cursor], r.m.pd.Repos[r.cursor+1:]...)
	r.save()
	r.rebuild()
	r.status = okStyle.Render(i18n.T("tui.repos.removed", name))
}

func (r *reposModel) save() {
	if err := projectdef.Save(r.m.cfg.ProjectDef, r.m.pd); err != nil {
		r.status = errStyle.Render(i18n.T("tui.repos.save_failed", err.Error()))
	}
}

// stateLabel is a localized label for a clone state ("" for a healthy clone).
func stateLabel(s repos.CloneState) string {
	switch s {
	case repos.StateMissing:
		return i18n.T("tui.repos.st_missing")
	case repos.StateNotGit:
		return i18n.T("tui.repos.st_notgit")
	case repos.StateWorktree:
		return i18n.T("tui.repos.st_worktree")
	default:
		return i18n.T("tui.repos.st_cloned")
	}
}

func (r *reposModel) View() string {
	if r.form != nil {
		title := i18n.T("tui.repos.add_title")
		if r.formMode == rfEditBranch {
			title = i18n.T("tui.repos.edit_branch_title")
		}
		return docStyle.Render(titleStyle.Render(title) + "\n\n" + r.form.View() + "\n" + helpStyle.Render(i18n.T("tui.form_help")))
	}

	var b strings.Builder
	b.WriteString(titleStyle.Render(i18n.T("tui.repos.title", r.m.cfg.ProjectName)) + "\n\n")

	if len(r.rows) == 0 {
		b.WriteString(dimStyle.Render(i18n.T("tui.repos.empty")) + "\n\n")
	} else {
		header := fmt.Sprintf("   %-18s %-16s %-16s %s",
			i18n.T("tui.col.repo"), i18n.T("tui.repos.col_default"), i18n.T("tui.repos.col_active"), i18n.T("tui.repos.col_status"))
		b.WriteString(dimStyle.Render(header) + "\n")
		for i, row := range r.rows {
			marker := "✓"
			if row.state != repos.StateClone {
				marker = "✗"
			}
			line := fmt.Sprintf("%s %-18s %-16s %-16s %s",
				marker, pad(row.name, 18), pad(orDash(row.branch), 16), pad(orDash(row.active), 16), stateLabel(row.state))
			switch {
			case i == r.cursor:
				line = selectedRowStyle.Render(line)
			case row.state != repos.StateClone:
				line = errStyle.Render(line) // not a real clone → red
			}
			// Flag a clone that's on a different branch than configured.
			if row.diverged() {
				line += "  " + warnStyle.Render(i18n.T("tui.repos.diverged", row.branch))
			}
			b.WriteString(line + "\n")
		}
	}

	b.WriteString("\n")
	if r.status != "" {
		b.WriteString(r.status + "\n")
	}
	b.WriteString(helpStyle.Render(i18n.T("tui.repos.help")))
	return docStyle.Render(b.String())
}

// pad truncates or right-pads s to exactly w runes (keeps columns aligned).
func pad(s string, w int) string {
	r := []rune(s)
	if len(r) > w {
		if w <= 1 {
			return string(r[:w])
		}
		return string(r[:w-1]) + "…"
	}
	return s + strings.Repeat(" ", w-len(r))
}
