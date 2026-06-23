package tui

import (
	"fmt"
	"strings"

	"github.com/JoshuaLM114/workwood/config"
	"github.com/JoshuaLM114/workwood/gitx"
	"github.com/JoshuaLM114/workwood/i18n"
	"github.com/JoshuaLM114/workwood/manifest"
	"github.com/JoshuaLM114/workwood/repos"
	"github.com/JoshuaLM114/workwood/superfeature"
	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
)

// formMode tracks which huh modal (if any) is open over the editor.
type formMode int

const (
	formNone        formMode = iota
	formAddMode              // phase 1: pick repo + new/from-existing
	formAdd                  // phase 2: a NEW branch (name + placement + source)
	formAddExisting          // phase 2: pick an EXISTING branch to check out
	formMeta
)

// editorBranchesMsg carries a repo's branches back to the editor after the async
// fetch kicked off when the user chose "from existing".
type editorBranchesMsg struct {
	repo     string
	branches []repos.BranchRef
}

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
// worktrees, lets the user stage additions and removals (and edit the name +
// description), then applies the whole delta at once on save. Targets are edited
// separately on the Actions screen (opened with `o`).
type editorModel struct {
	m      *Model
	name   string // feature slug (immutable handle)
	active string // active_name (display)

	man   *manifest.Manifest
	desc  string
	rows  []editorRow
	table table.Model

	form     *huh.Form
	formMode formMode
	addVals  addVals
	metaVals metaVals

	status string
	busy   bool
	width  int
	height int
	desync *superfeature.DiagnoseResult // manifest↔disk drift, refreshed on open/apply
}

// checkDesync refreshes the manifest↔disk diagnosis (best-effort; nil/clean when in
// sync). Cheap: a dir scan + a stat per worktree.
func (e *editorModel) checkDesync() {
	d, err := superfeature.Diagnose(e.m.cfg, e.m.pd, e.name)
	if err != nil || d.OK() {
		e.desync = nil
		return
	}
	e.desync = d
}

// newEditorModel loads the feature (by slug) and builds its table.
func newEditorModel(m *Model, slug string) (*editorModel, error) {
	man, err := manifest.Load(m.cfg.ManifestPath(slug))
	if err != nil {
		return nil, err
	}
	active := slug
	if man.ID != "" {
		if ps, err := config.LoadState(m.cfg.StateFile); err == nil {
			if fs, ok := ps.FeatureByUUID(man.ID); ok {
				active = fs.DisplayName()
			}
		}
	}
	e := &editorModel{m: m, name: slug, active: active, man: man, desc: man.Description}
	e.table = table.New(
		table.WithColumns(editorColumns(m.width)),
		table.WithFocused(true),
	)
	e.table.SetStyles(tableStyles())
	e.rebuildRows()
	e.checkDesync()
	return e, nil
}

func editorColumns(width int) []table.Column {
	branchW := 36
	if width > 0 {
		branchW = max(16, width-46)
	}
	return []table.Column{
		{Title: "", Width: 9},
		{Title: i18n.T("tui.col.repo"), Width: 14},
		{Title: i18n.T("tui.col.branch"), Width: branchW},
		{Title: i18n.T("tui.col.base"), Width: 9},
	}
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
		return table.Row{"+ add", r.add.Repo, r.addBranch, orDash(r.add.From)}
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
		return table.Row{status, r.wt.Repo, r.wt.Branch, r.wt.Base}
	}
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
			return e, e.onFormDone() // may open the next phase (its Init cmd)
		} else if e.form.State == huh.StateAborted {
			e.form = nil
			e.formMode = formNone
		}
		return e, cmd
	}

	switch msg := msg.(type) {
	case editorBranchesMsg:
		e.busy = false
		if len(msg.branches) == 0 {
			e.status = warnStyle.Render(i18n.T("tui.status.no_branches", msg.repo))
			return e, nil
		}
		e.form = newAddExistingForm(msg.branches, &e.addVals).WithWidth(min(72, e.width-2))
		e.formMode = formAddExisting
		return e, e.form.Init()

	case applyDoneMsg:
		e.busy = false
		// Reload either way: ApplyEdit persists each success even when a later op
		// fails, so the manifest now reflects reality. Succeeded adds become tracked
		// rows; the failed/unprocessed ones stay staged to fix + retry.
		e.reloadAfterApply(msg.res, msg.err)
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
		case "o":
			slug := e.name
			return e, func() tea.Msg { return openActionsMsg{feature: slug} }
		case "D":
			if e.desync == nil {
				e.status = okStyle.Render(i18n.T("tui.editor.in_sync"))
				return e, nil
			}
			slug := e.name
			return e, func() tea.Msg { return openReconcileMsg{feature: slug} }
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
	e.form = newAddModeForm(e.m.pd.Names(), &e.addVals).WithWidth(min(72, e.width-2))
	e.formMode = formAddMode
}

// fetchBranchesCmd refreshes a repo's remote refs then lists its branches, so the
// "from existing" dropdown reflects teammates' pushed branches.
func (e *editorModel) fetchBranchesCmd(repo string) tea.Cmd {
	base := e.m.cfg.BaseRepo(repo)
	return func() tea.Msg {
		gitx.Fetch(base)
		return editorBranchesMsg{repo: repo, branches: repos.Branches(base)}
	}
}

func (e *editorModel) openMetaForm() {
	e.metaVals = metaVals{name: e.active, desc: e.desc}
	e.form = newMetaForm(&e.metaVals).WithWidth(min(72, e.width-2))
	e.formMode = formMeta
}

// applyRename persists a new active_name locally (keyed by the feature UUID). It
// changes only the display label — never the slug, manifest filename, or branches.
func (e *editorModel) applyRename(newName string) {
	if newName == "" || e.man.ID == "" || newName == e.active {
		return
	}
	st, err := config.LoadState(e.m.cfg.StateFile)
	if err != nil {
		return
	}
	st.EnsureFeature(e.man.ID, e.name)
	f := st.Features[e.man.ID]
	f.Name = newName
	st.Features[e.man.ID] = f
	if config.SaveState(e.m.cfg.StateFile, st) == nil {
		e.active = newName
	}
}

// onFormDone reads back the completed form and applies the result — or advances to
// the next phase. It returns any follow-up command (a phase-2 form's Init, or the
// branch fetch for "from existing").
func (e *editorModel) onFormDone() tea.Cmd {
	switch e.formMode {
	case formAddMode:
		if e.addVals.fromExisting {
			// Phase 2 (existing): fetch the repo's branches, then show the dropdown.
			e.form = nil
			e.formMode = formNone
			e.busy = true
			e.status = i18n.T("tui.status.fetching_branches")
			return e.fetchBranchesCmd(e.addVals.repo)
		}
		// Phase 2 (new): collect the new branch name + placement + source.
		e.form = newAddForm(e.man.BranchPrefix(), &e.addVals).WithWidth(min(72, e.width-2))
		e.formMode = formAdd
		return e.form.Init()
	case formAdd:
		sub := strings.TrimSpace(e.addVals.sub)
		e.stageAdd(superfeature.AddSpec{
			Repo:              e.addVals.repo,
			Sub:               sub,
			From:              strings.TrimSpace(e.addVals.from),
			OmitFeaturePrefix: e.addVals.omitPrefix,
		})
	case formAddExisting:
		// Check out the chosen branch directly (no <feature>/ prefix) — addWorktree
		// attaches to the local/remote branch.
		if branch := strings.TrimSpace(e.addVals.branch); branch != "" {
			e.stageAdd(superfeature.AddSpec{Repo: e.addVals.repo, Sub: branch, OmitFeaturePrefix: true})
		}
	case formMeta:
		e.desc = e.metaVals.desc
		e.applyRename(strings.TrimSpace(e.metaVals.name))
		e.status = i18n.T("tui.status.desc_updated")
	}
	e.form = nil
	e.formMode = formNone
	return nil
}

// stageAdd appends a staged worktree-add row from a resolved spec.
func (e *editorModel) stageAdd(add superfeature.AddSpec) {
	e.rows = append(e.rows, editorRow{
		kind:      rowStagedAdd,
		add:       add,
		addBranch: superfeature.ResolveBranchWith(e.man.BranchPrefix(), add.Sub, add.OmitFeaturePrefix),
	})
	e.syncTable()
	e.status = i18n.T("tui.status.staged_add")
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
		// nil onNew: auto-create new local branches. The editor runs off the event
		// loop and can't prompt mid-rebuild; in the editor you own the feature, so
		// creating its branches is expected. (The CLI `sf up` prompts.)
		log, err := superfeature.Up(cfg, name, nil)
		return upDoneMsg{log: log, err: err}
	}
}

// reloadAfterApply re-reads the manifest, reconciles staging, and reports the
// delta. applyErr is non-nil on a partial apply: the manifest already records what
// succeeded, so we drop staged adds the manifest now contains (they landed) and
// keep the rest staged to fix + retry. rebuildRows preserves staged adds + remove
// flags, so the only thing to prune here is the succeeded adds.
func (e *editorModel) reloadAfterApply(res *superfeature.EditResult, applyErr error) {
	man, err := manifest.Load(e.m.cfg.ManifestPath(e.name))
	if err != nil {
		e.status = errStyle.Render(i18n.T("tui.status.reload_failed", err.Error()))
		return
	}
	e.man = man
	e.desc = man.Description
	kept := e.rows[:0]
	for _, r := range e.rows {
		if r.kind == rowStagedAdd && man.Find(r.add.Repo, r.addBranch) >= 0 {
			continue // this add landed in the manifest → no longer staged
		}
		kept = append(kept, r)
	}
	e.rows = kept
	e.rebuildRows()
	e.checkDesync()
	if applyErr != nil {
		e.status = errStyle.Render(i18n.T("tui.status.apply_partial", applyErr.Error()))
		return
	}
	added, removed := 0, 0
	if res != nil {
		added, removed = len(res.Added), len(res.Removed)
	}
	e.status = okStyle.Render(i18n.T("tui.status.saved", added, removed))
}

func (e *editorModel) View() string {
	if e.form != nil {
		title := i18n.T("tui.title.add_worktree")
		if e.formMode == formMeta {
			title = i18n.T("tui.title.edit_desc")
		}
		return docStyle.Render(titleStyle.Render(title) + "\n\n" + e.form.View() + "\n" + helpStyle.Render(i18n.T("tui.form_help")))
	}

	var b strings.Builder
	header := fmt.Sprintf("%s  %s", titleStyle.Render(e.active), dimStyle.Render(orDash(e.desc)))
	b.WriteString(header + "\n\n")
	b.WriteString(e.table.View() + "\n\n")

	if e.status != "" {
		b.WriteString(e.status + "\n")
	}
	if e.desync != nil {
		b.WriteString(warnStyle.Render(i18n.T("tui.editor.desync", len(e.desync.Orphans), len(e.desync.Missing))) + "\n")
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
