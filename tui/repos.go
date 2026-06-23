package tui

import (
	"fmt"
	"strings"

	"github.com/JoshuaLM114/workwood/i18n"
	"github.com/JoshuaLM114/workwood/projectdef"
	"github.com/JoshuaLM114/workwood/repos"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
)

type reposFormMode int

const (
	rfNone       reposFormMode = iota
	rfAdd                      // step 1: repo name + url
	rfAddBranch                // step 2: pick the default branch (remote branches fetched)
	rfEditBranch               // edit an existing repo's default branch
)

type reposDoneMsg struct {
	log []string
	err error
}

// addBranchesMsg carries the remote branches fetched for a repo being added (nil
// if listing failed — step 2 then falls back to a free-text branch entry).
type addBranchesMsg struct{ branches []repos.BranchRef }

// repoRow is one base repo plus its on-disk clone state, active branch, and sync
// position relative to origin.
type repoRow struct {
	name   string
	branch string // configured default branch (workwood.yml)
	active string // branch actually checked out in the base clone ("" if N/A)
	state  repos.CloneState
	sync   repos.SyncInfo
}

// diverged reports whether the clone is on a different branch than configured.
func (row repoRow) diverged() bool {
	return row.state == repos.StateClone && row.active != "" && row.active != row.branch
}

// statusText is the last column: the clone-state label for non-clones, else the
// sync position relative to origin (refreshed by the on-boot fetch).
func (row repoRow) statusText() string {
	if row.state != repos.StateClone {
		return stateLabel(row.state)
	}
	s := row.sync
	switch {
	case !s.HasUpstream:
		return i18n.T("tui.repos.sync_no_upstream")
	case s.Behind > 0 && s.Ahead > 0:
		return i18n.T("tui.repos.sync_diverged", s.Ahead, s.Behind)
	case s.Behind > 0:
		return i18n.T("tui.repos.sync_behind", s.Behind)
	case s.Ahead > 0:
		return i18n.T("tui.repos.sync_ahead", s.Ahead)
	default:
		return i18n.T("tui.repos.sync_uptodate")
	}
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
	spinner    spinner.Model
	width      int
	height     int
}

func newReposModel(m *Model) *reposModel {
	r := &reposModel{m: m}
	r.spinner = spinner.New(spinner.WithSpinner(spinner.Dot))
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
			sync:   repos.AheadBehind(base),
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
			return r, r.onFormDone() // may kick off the step-2 branch fetch
		} else if r.form.State == huh.StateAborted {
			r.form = nil
			r.formMode = rfNone
		}
		return r, cmd
	}

	switch msg := msg.(type) {
	case spinner.TickMsg:
		if !r.busy {
			return r, nil
		}
		var cmd tea.Cmd
		r.spinner, cmd = r.spinner.Update(msg)
		return r, cmd

	case addBranchesMsg:
		// Step 1 finished and branches are in → open step 2 (branch picker), or a
		// free-text fallback when listing came back empty.
		r.busy = false
		r.branchVals = branchVals{branch: "main"}
		r.form = newEditBranchForm(msg.branches, &r.branchVals).WithWidth(min(72, r.width-2))
		r.formMode = rfAddBranch
		return r, r.form.Init()

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
			r.repoVals = repoVals{}
			r.form = newAddRepoForm(&r.repoVals).WithWidth(min(72, r.width-2))
			r.formMode = rfAdd
			return r, r.form.Init()
		case "e":
			if r.cursor >= 0 && r.cursor < len(r.m.pd.Repos) {
				repo := r.m.pd.Repos[r.cursor]
				r.branchVals = branchVals{branch: repo.DefaultBranch}
				branches := repos.Branches(r.m.cfg.BaseRepo(repo.Name))
				r.form = newEditBranchForm(branches, &r.branchVals).WithWidth(min(72, r.width-2))
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
			return r, tea.Batch(r.spinner.Tick, r.syncCmd())
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

// onFormDone handles a completed form and returns any follow-up command. rfAdd
// (step 1) doesn't create the repo — it validates the name, then fetches the
// remote's branches so step 2 can offer a branch dropdown.
func (r *reposModel) onFormDone() tea.Cmd {
	switch r.formMode {
	case rfAdd:
		return r.beginAdd()
	case rfAddBranch:
		r.addRepo()
	case rfEditBranch:
		r.editBranch()
	}
	r.form = nil
	r.formMode = rfNone
	return nil
}

// beginAdd validates the new repo name then kicks off the remote-branch fetch.
func (r *reposModel) beginAdd() tea.Cmd {
	r.form = nil
	r.formMode = rfNone
	name := strings.TrimSpace(r.repoVals.name)
	if name == "" {
		return nil
	}
	for _, e := range r.m.pd.Repos {
		if e.Name == name {
			r.status = errStyle.Render(i18n.T("tui.repos.exists", name))
			return nil
		}
	}
	r.busy = true
	r.status = i18n.T("tui.repos.fetching_branches", name)
	url := strings.TrimSpace(r.repoVals.url)
	return tea.Batch(r.spinner.Tick, func() tea.Msg {
		return addBranchesMsg{branches: repos.RemoteBranchesFor(url)}
	})
}

// addRepo (step 2) appends the repo with the chosen default branch.
func (r *reposModel) addRepo() {
	name := strings.TrimSpace(r.repoVals.name)
	if name == "" {
		return
	}
	branch := strings.TrimSpace(r.branchVals.branch)
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
	if r.busy {
		msg := r.status
		if strings.TrimSpace(msg) == "" {
			msg = i18n.T("tui.editor.working")
		}
		body := r.spinner.View() + "  " + msg
		return docStyle.Render(titleStyle.Render(i18n.T("tui.editor.loading")) + "\n\n" + body + "\n\n" + helpStyle.Render(i18n.T("tui.editor.loading_hint")))
	}
	if r.form != nil {
		title := i18n.T("tui.repos.add_title")
		if r.formMode == rfEditBranch || r.formMode == rfAddBranch {
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
			prefix := fmt.Sprintf("%s %-18s %-16s %-16s ",
				marker, pad(row.name, 18), pad(orDash(row.branch), 16), pad(orDash(row.active), 16))
			status := row.statusText()
			var line string
			switch {
			case i == r.cursor:
				line = selectedRowStyle.Render(prefix + status)
			case row.state != repos.StateClone:
				line = errStyle.Render(prefix + status) // not a real clone → red
			case row.sync.OutOfSync():
				line = prefix + warnStyle.Render(status) // behind origin → amber
			default:
				line = prefix + status
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
