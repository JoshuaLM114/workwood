package tui

import (
	"fmt"
	"strings"

	"github.com/JoshuaLM114/workwood/i18n"
	"github.com/JoshuaLM114/workwood/manifest"
	"github.com/JoshuaLM114/workwood/superfeature"
	"github.com/JoshuaLM114/workwood/targets"
	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
)

// formMode tracks which huh modal (if any) is open over the editor.
type formMode int

const (
	formNone formMode = iota
	formAdd
	formMeta
	formTarget
	formBulk
)

// rowKind distinguishes a persisted worktree from one staged for addition.
type rowKind int

const (
	rowExisting rowKind = iota
	rowStagedAdd
)

// editorRow is one line in the editor table, kept parallel to table rows so the
// selected row maps back to an action.
type editorRow struct {
	kind       rowKind
	wt         manifest.Worktree    // rowExisting
	add        superfeature.AddSpec // rowStagedAdd
	addBranch  string               // rowStagedAdd: previewed full branch
	checkedOut bool                 // rowExisting
	removing   bool                 // rowExisting: staged for removal
	prune      bool                 // rowExisting: also delete branch on save
}

// editorModel is the malleable feature editor. It loads a feature's current
// worktrees, lets the user stage additions and removals (and edit the
// description), then applies the whole delta at once on save.
type editorModel struct {
	m    *Model
	name string

	man   *manifest.Manifest
	desc  string
	rows  []editorRow
	table table.Model
	state *targets.State

	form       *huh.Form
	formMode   formMode
	addVals    addVals
	metaVals   metaVals
	setupVals  setupVals
	bulkVals   bulkVals
	targetRepo string // repo being targeted while formTarget is open

	status string
	busy   bool
	width  int
	height int
}

// newEditorModel loads the feature and builds its table.
func newEditorModel(m *Model, name string) (*editorModel, error) {
	man, err := manifest.Load(m.cfg.ManifestPath(name))
	if err != nil {
		return nil, err
	}
	st, err := superfeature.LoadState(m.cfg, name)
	if err != nil {
		return nil, err
	}
	e := &editorModel{m: m, name: name, man: man, desc: man.Description, state: st}
	e.table = table.New(
		table.WithColumns(editorColumns(m.width)),
		table.WithFocused(true),
	)
	e.table.SetStyles(tableStyles())
	e.rebuildRows()
	return e, nil
}

func editorColumns(width int) []table.Column {
	branchW := 30
	targetW := 22
	if width > 0 {
		branchW = max(14, width-72)
	}
	return []table.Column{
		{Title: "", Width: 9},
		{Title: i18n.T("tui.col.repo"), Width: 14},
		{Title: i18n.T("tui.col.branch"), Width: branchW},
		{Title: i18n.T("tui.col.base"), Width: 9},
		{Title: i18n.T("tui.col.targets"), Width: targetW},
	}
}

// targetLabel is the comma-joined list of a repo's setups, or "auto".
func (e *editorModel) targetLabel(repo string) string {
	return targets.LabelList(e.state.TargetsFor(repo), e.name)
}

// dirty reports whether there are unsaved staged changes.
func (e *editorModel) dirty() bool {
	if e.desc != e.man.Description {
		return true
	}
	for _, r := range e.rows {
		if r.kind == rowStagedAdd || (r.kind == rowExisting && r.removing) {
			return true
		}
	}
	return false
}

// rebuildRows recomputes the editor rows from the manifest + staged state and
// feeds them to the table, preserving any existing staged adds/removes.
func (e *editorModel) rebuildRows() {
	staged := []editorRow{}
	removing := map[string]editorRow{}
	for _, r := range e.rows {
		if r.kind == rowStagedAdd {
			staged = append(staged, r)
		} else if r.removing {
			removing[r.wt.Repo+"\x00"+r.wt.Branch] = r
		}
	}

	rows := make([]editorRow, 0, len(e.man.Worktrees)+len(staged))
	for _, w := range e.man.Worktrees {
		row := editorRow{kind: rowExisting, wt: w}
		if pathExists(e.m.cfg.Abs(w.Path)) {
			row.checkedOut = true
		}
		if prev, ok := removing[w.Repo+"\x00"+w.Branch]; ok {
			row.removing = true
			row.prune = prev.prune
		}
		rows = append(rows, row)
	}
	rows = append(rows, staged...)
	e.rows = rows

	tr := make([]table.Row, 0, len(rows))
	for _, r := range rows {
		tr = append(tr, e.tableRow(r))
	}
	e.table.SetRows(tr)
}

func (e *editorModel) tableRow(r editorRow) table.Row {
	switch r.kind {
	case rowStagedAdd:
		return table.Row{"+ add", r.add.Repo, r.addBranch, orDash(r.add.From), e.targetLabel(r.add.Repo)}
	default:
		status := "·"
		if r.checkedOut {
			status = "✓"
		}
		if r.removing {
			status = "✗ remove"
			if r.prune {
				status = "✗ +prune"
			}
		}
		return table.Row{status, r.wt.Repo, r.wt.Branch, r.wt.Base, e.targetLabel(r.wt.Repo)}
	}
}

// rowRepo returns the repo of a row (existing worktree or staged add).
func (r editorRow) rowRepo() string {
	if r.kind == rowStagedAdd {
		return r.add.Repo
	}
	return r.wt.Repo
}

func (e *editorModel) setSize(w, h int) {
	e.width, e.height = w, h
	e.table.SetColumns(editorColumns(w))
	e.table.SetWidth(w)
	e.table.SetHeight(max(3, h-9))
}

func (e *editorModel) Update(msg tea.Msg) (*editorModel, tea.Cmd) {
	// A huh modal, when open, owns input — EXCEPT esc (cancel back to the editor)
	// and ctrl+c (quit), so the user is never locked inside a form.
	if e.form != nil {
		if k, ok := msg.(tea.KeyMsg); ok {
			switch k.String() {
			case "esc":
				e.form = nil
				e.formMode = formNone
				e.status = i18n.T("tui.status.cancelled")
				return e, nil
			case "ctrl+c":
				return e, tea.Quit
			}
		}
		fm, cmd := e.form.Update(msg)
		if f, ok := fm.(*huh.Form); ok {
			e.form = f
		}
		if e.form.State == huh.StateCompleted {
			e.onFormDone()
		} else if e.form.State == huh.StateAborted {
			e.form = nil
			e.formMode = formNone
		}
		return e, cmd
	}

	switch msg := msg.(type) {
	case applyDoneMsg:
		e.busy = false
		if msg.err != nil {
			e.status = errStyle.Render(i18n.T("tui.status.apply_failed", msg.err.Error()))
			return e, nil
		}
		e.reloadAfterApply(msg.res)
		return e, nil

	case upDoneMsg:
		e.busy = false
		if msg.err != nil {
			e.status = errStyle.Render(i18n.T("tui.status.rebuild_failed", msg.err.Error()))
			return e, nil
		}
		e.rebuildRows()
		e.status = okStyle.Render(i18n.T("tui.status.rebuilt", len(msg.log)))
		return e, nil

	case tea.KeyMsg:
		if e.busy {
			return e, nil
		}
		switch msg.String() {
		case "q", "ctrl+c":
			return e, tea.Quit
		case "esc":
			return e, func() tea.Msg { return backMsg{} }
		case "a":
			e.openAddForm()
			return e, e.form.Init()
		case "e":
			e.openMetaForm()
			return e, e.form.Init()
		case "d", "x":
			e.toggleRemove(false)
			return e, nil
		case "X":
			e.toggleRemove(true)
			return e, nil
		case "t":
			if repo := e.selectedRepo(); repo != "" {
				e.openTargetForm(repo)
				return e, e.form.Init()
			}
			return e, nil
		case "T":
			e.openBulkForm()
			if e.form != nil {
				return e, e.form.Init()
			}
			return e, nil
		case "c":
			e.clearSelectedTarget()
			return e, nil
		case "s":
			if !e.dirty() {
				e.status = i18n.T("tui.status.nothing")
				return e, nil
			}
			e.busy = true
			e.status = i18n.T("tui.status.applying")
			return e, e.applyCmd()
		case "u":
			e.busy = true
			e.status = i18n.T("tui.status.rebuilding")
			return e, e.upCmd()
		}
	}

	var cmd tea.Cmd
	e.table, cmd = e.table.Update(msg)
	return e, cmd
}

// toggleRemove flips removal staging on the selected row.
func (e *editorModel) toggleRemove(prune bool) {
	i := e.table.Cursor()
	if i < 0 || i >= len(e.rows) {
		return
	}
	r := e.rows[i]
	if r.kind == rowStagedAdd {
		e.rows = append(e.rows[:i], e.rows[i+1:]...)
		e.syncTable()
		e.status = i18n.T("tui.status.dropped_add")
		return
	}
	if r.removing && r.prune == prune {
		e.rows[i].removing = false
		e.rows[i].prune = false
	} else {
		e.rows[i].removing = true
		e.rows[i].prune = prune
	}
	e.syncTable()
}

// syncTable re-renders table rows from e.rows without reloading the manifest.
func (e *editorModel) syncTable() {
	tr := make([]table.Row, 0, len(e.rows))
	for _, r := range e.rows {
		tr = append(tr, e.tableRow(r))
	}
	e.table.SetRows(tr)
}

func (e *editorModel) openAddForm() {
	e.addVals = addVals{}
	e.form = newAddForm(e.m.pd.Names(), &e.addVals).WithWidth(min(72, e.width-2))
	e.formMode = formAdd
}

func (e *editorModel) openMetaForm() {
	e.metaVals = metaVals{desc: e.desc}
	e.form = newMetaForm(&e.metaVals).WithWidth(min(72, e.width-2))
	e.formMode = formMeta
}

// selectedRepo is the repo of the highlighted table row, or "".
func (e *editorModel) selectedRepo() string {
	i := e.table.Cursor()
	if i < 0 || i >= len(e.rows) {
		return ""
	}
	return e.rows[i].rowRepo()
}

// worktreeOpts returns the worktree picker options for a repo.
func (e *editorModel) worktreeOpts(repo string) []targetOption {
	var opts []targetOption
	for _, w := range e.man.Worktrees {
		if w.Repo == repo {
			opts = append(opts, targetOption{
				label: i18n.T("tui.opt.worktree", strings.TrimPrefix(w.Path, e.name+"/")),
				value: "wt:" + w.Path,
			})
		}
	}
	return opts
}

// featureRepos lists the distinct repos in the feature, in manifest order.
func (e *editorModel) featureRepos() []string {
	seen := map[string]bool{}
	var out []string
	for _, w := range e.man.Worktrees {
		if !seen[w.Repo] {
			seen[w.Repo] = true
			out = append(out, w.Repo)
		}
	}
	return out
}

func (e *editorModel) openTargetForm(repo string) {
	e.targetRepo = repo
	e.setupVals = setupVals{}
	e.form = newAddTargetForm(repo, e.worktreeOpts(repo), &e.setupVals).WithWidth(min(72, e.width-2))
	e.formMode = formTarget
}

func (e *editorModel) openBulkForm() {
	repos := e.featureRepos()
	if len(repos) == 0 {
		e.status = i18n.T("tui.status.no_repos")
		return
	}
	e.bulkVals = bulkVals{}
	e.form = newBulkTargetForm(repos, &e.bulkVals).WithWidth(min(72, e.width-2))
	e.formMode = formBulk
}

func (e *editorModel) clearSelectedTarget() {
	repo := e.selectedRepo()
	if repo == "" {
		return
	}
	if err := superfeature.ClearTarget(e.m.cfg, e.name, repo); err != nil {
		e.status = errStyle.Render(i18n.T("tui.status.clear_failed", err.Error()))
		return
	}
	e.reloadState()
	e.status = okStyle.Render(i18n.T("tui.status.cleared", repo))
}

// parseSetup turns a setup picker result into a targets.Target.
func parseSetup(v setupVals) (targets.Target, bool) {
	switch {
	case v.choice == "custom":
		c := strings.TrimSpace(v.custom)
		if c == "" {
			return targets.Target{}, false
		}
		return targets.Target{Source: targets.Source(c)}, true
	case v.choice == "ignore":
		return targets.Target{Source: targets.SourceIgnore}, true
	case v.choice == "main":
		return targets.Target{Source: targets.SourceMain}, true
	case v.choice == "worktree":
		return targets.Target{Source: targets.SourceWorktree}, true
	case strings.HasPrefix(v.choice, "wt:"):
		return targets.Target{Source: targets.SourceWorktree, Worktree: strings.TrimPrefix(v.choice, "wt:")}, true
	}
	return targets.Target{}, false
}

// onFormDone reads back the completed form and applies the result.
func (e *editorModel) onFormDone() {
	switch e.formMode {
	case formAdd:
		sub := strings.TrimSpace(e.addVals.sub)
		add := superfeature.AddSpec{
			Repo:              e.addVals.repo,
			Sub:               sub,
			From:              strings.TrimSpace(e.addVals.from),
			OmitFeaturePrefix: e.addVals.omitPrefix,
		}
		e.rows = append(e.rows, editorRow{
			kind:      rowStagedAdd,
			add:       add,
			addBranch: superfeature.ResolveBranchWith(e.name, sub, add.OmitFeaturePrefix),
		})
		e.syncTable()
		e.status = i18n.T("tui.status.staged_add")
	case formMeta:
		e.desc = e.metaVals.desc
		e.status = i18n.T("tui.status.desc_updated")
	case formTarget:
		if t, ok := parseSetup(e.setupVals); ok {
			added, err := superfeature.AddTarget(e.m.cfg, e.name, e.targetRepo, t)
			if err != nil {
				e.status = errStyle.Render(i18n.T("tui.status.target_failed", err.Error()))
			} else {
				e.reloadState()
				if added {
					e.status = okStyle.Render(i18n.T("tui.status.target_added", e.targetRepo, t.Label(e.name)))
				} else {
					e.status = i18n.T("tui.status.target_exists", e.targetRepo, t.Label(e.name))
				}
			}
		}
	case formBulk:
		if t, ok := parseSetup(e.bulkVals.setup); ok {
			n := 0
			for _, repo := range e.bulkVals.repos {
				if _, err := superfeature.AddTarget(e.m.cfg, e.name, repo, t); err != nil {
					e.status = errStyle.Render(i18n.T("tui.status.bulk_failed", err.Error()))
					e.form = nil
					e.formMode = formNone
					return
				}
				n++
			}
			e.reloadState()
			e.status = okStyle.Render(i18n.T("tui.status.bulk_added", t.Label(e.name), n))
		}
	}
	e.form = nil
	e.formMode = formNone
}

// reloadState re-reads the per-developer target state and re-renders.
func (e *editorModel) reloadState() {
	if st, err := superfeature.LoadState(e.m.cfg, e.name); err == nil {
		e.state = st
	}
	e.syncTable()
}

// applyCmd runs the staged delta off the event loop.
func (e *editorModel) applyCmd() tea.Cmd {
	name := e.name
	desc := e.desc
	var adds []superfeature.AddSpec
	var removes []superfeature.RemoveSpec
	for _, r := range e.rows {
		switch {
		case r.kind == rowStagedAdd:
			adds = append(adds, r.add)
		case r.kind == rowExisting && r.removing:
			removes = append(removes, superfeature.RemoveSpec{
				Repo:        r.wt.Repo,
				Branch:      r.wt.Branch,
				PruneBranch: r.prune,
			})
		}
	}
	cfg, pd := e.m.cfg, e.m.pd
	return func() tea.Msg {
		res, err := superfeature.ApplyEdit(cfg, pd, name, desc, adds, removes)
		return applyDoneMsg{res: res, err: err}
	}
}

func (e *editorModel) upCmd() tea.Cmd {
	cfg, name := e.m.cfg, e.name
	return func() tea.Msg {
		log, err := superfeature.Up(cfg, name)
		return upDoneMsg{log: log, err: err}
	}
}

// reloadAfterApply re-reads the manifest, clears staging, and reports the delta.
func (e *editorModel) reloadAfterApply(res *superfeature.EditResult) {
	man, err := manifest.Load(e.m.cfg.ManifestPath(e.name))
	if err != nil {
		e.status = errStyle.Render(i18n.T("tui.status.reload_failed", err.Error()))
		return
	}
	e.man = man
	e.desc = man.Description
	e.rows = nil
	e.rebuildRows()
	added, removed := 0, 0
	if res != nil {
		added, removed = len(res.Added), len(res.Removed)
	}
	e.status = okStyle.Render(i18n.T("tui.status.saved", added, removed))
}

func (e *editorModel) View() string {
	if e.form != nil {
		title := i18n.T("tui.title.add_worktree")
		switch e.formMode {
		case formMeta:
			title = i18n.T("tui.title.edit_desc")
		case formTarget:
			title = i18n.T("tui.title.add_target")
		case formBulk:
			title = i18n.T("tui.title.bulk")
		}
		return docStyle.Render(titleStyle.Render(title) + "\n\n" + e.form.View() + "\n" + helpStyle.Render(i18n.T("tui.form_help")))
	}

	var b strings.Builder
	header := fmt.Sprintf("%s  %s", titleStyle.Render(e.name), dimStyle.Render(orDash(e.desc)))
	b.WriteString(header + "\n\n")
	b.WriteString(e.table.View() + "\n\n")

	if e.status != "" {
		b.WriteString(e.status + "\n")
	}
	dirtyHint := ""
	if e.dirty() {
		dirtyHint = warnStyle.Render(i18n.T("tui.unsaved"))
	}
	b.WriteString(helpStyle.Render(i18n.T("tui.help") + dirtyHint))
	return docStyle.Render(b.String())
}

func orDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "—"
	}
	return s
}
