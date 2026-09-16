package menus

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"

	"github.com/JoshuaLM114/workwood/config"
	"github.com/JoshuaLM114/workwood/i18n"
	"github.com/JoshuaLM114/workwood/libs/gitx"
	"github.com/JoshuaLM114/workwood/manifest"
	"github.com/JoshuaLM114/workwood/models"
	"github.com/JoshuaLM114/workwood/repos"
	"github.com/JoshuaLM114/workwood/superfeature"
	"github.com/JoshuaLM114/workwood/tui/components"
)

// pathExists reports whether a filesystem path exists.
func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

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
// fetch kicked off when the user chooses a repo and branch mode.
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
	wt         models.Worktree      // rowExisting
	add        superfeature.AddSpec // rowStagedAdd
	addBranch  string               // rowStagedAdd: previewed full branch
	checkedOut bool                 // rowExisting
	removing   bool                 // rowExisting: staged for removal
	prune      bool                 // rowExisting: also delete branch on save
}

// EditorModel is the malleable feature editor. It loads a feature's current
// worktrees, lets the user stage additions and removals (and edit the name +
// description), then applies the whole delta at once on save. Targets are edited
// separately on the Actions screen (opened with `o`).
type EditorModel struct {
	ctx    Ctx
	name   string // feature slug (immutable handle)
	active string // active_name (display)

	man   *models.Manifest
	desc  string
	rows  []editorRow
	table table.Model

	form     *huh.Form
	formMode formMode
	addVals  addVals
	metaVals metaVals

	status  string
	busy    bool
	spinner spinner.Model
	width   int
	height  int
	desync  *superfeature.DiagnoseResult // manifest↔disk drift, refreshed on open/apply
}

// enterBusy switches the editor into its loading screen (status = what's happening)
// and starts the spinner alongside the async cmd.
func (e *EditorModel) enterBusy(status string, cmd tea.Cmd) tea.Cmd {
	e.busy = true
	e.status = status
	return tea.Batch(e.spinner.Tick, cmd)
}

// checkDesync refreshes the manifest↔disk diagnosis (best-effort; nil/clean when in
// sync). Cheap: a dir scan + a stat per worktree.
func (e *EditorModel) checkDesync() {
	d, err := superfeature.Diagnose(e.ctx.Cfg, e.ctx.Pd, e.name)
	if err != nil || d.OK() {
		e.desync = nil
		return
	}
	e.desync = d
}

// NewEditor loads the feature (by slug) and builds its table.
func NewEditor(ctx Ctx, slug string) (*EditorModel, error) {
	man, err := manifest.Load(ctx.Cfg.ManifestPath(slug))
	if err != nil {
		return nil, err
	}
	active := slug
	if man.ID != "" {
		if ps, err := config.LoadState(ctx.Cfg.StateFile); err == nil {
			if fs, ok := ps.FeatureByUUID(man.ID); ok {
				active = fs.DisplayName()
			}
		}
	}
	e := &EditorModel{ctx: ctx, name: slug, active: active, man: man, desc: man.Description}
	e.table = table.New(
		table.WithColumns(editorColumns(ctx.Width)),
		table.WithFocused(true),
	)
	e.table.SetStyles(components.TableStyles())
	e.rebuildRows()
	e.checkDesync()
	e.spinner = spinner.New(spinner.WithSpinner(spinner.Dot))
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
func (e *EditorModel) dirty() bool {
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
func (e *EditorModel) rebuildRows() {
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
		if pathExists(e.ctx.Cfg.Abs(w.Path)) {
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

func (e *EditorModel) tableRow(r editorRow) table.Row {
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

func (e *EditorModel) SetSize(w, h int) {
	e.width, e.height = w, h
	e.table.SetColumns(editorColumns(w))
	e.table.SetWidth(w)
	e.table.SetHeight(max(3, h-9))
}

func (e *EditorModel) Update(msg tea.Msg) (*EditorModel, tea.Cmd) {
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
	case spinner.TickMsg:
		if !e.busy {
			return e, nil // stop ticking once the async op finished
		}
		var cmd tea.Cmd
		e.spinner, cmd = e.spinner.Update(msg)
		return e, cmd

	case editorBranchesMsg:
		e.busy = false
		if !e.addVals.fromExisting {
			e.form = newAddForm(e.man.BranchPrefix(), &e.addVals, func(sub string, omit bool) error {
				return e.validateAdd(superfeature.AddSpec{Repo: e.addVals.repo, Sub: sub, OmitFeaturePrefix: omit})
			}).WithWidth(min(72, e.width-2))
			e.formMode = formAdd
			return e, e.form.Init()
		}
		if len(msg.branches) == 0 {
			e.status = components.WarnStyle.Render(i18n.T("tui.status.no_branches", msg.repo))
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
			e.status = components.ErrStyle.Render(i18n.T("tui.status.rebuild_failed", msg.err.Error()))
			return e, nil
		}
		e.rebuildRows()
		e.status = components.OkStyle.Render(i18n.T("tui.status.rebuilt", len(msg.log)))
		return e, nil

	case tea.KeyMsg:
		if e.busy {
			return e, nil
		}
		switch msg.String() {
		case "q", "ctrl+c":
			return e, tea.Quit
		case "esc":
			return e, func() tea.Msg { return BackMsg{} }
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
			return e, func() tea.Msg { return OpenActionsMsg{Feature: slug} }
		case "D":
			if e.desync == nil {
				e.status = components.OkStyle.Render(i18n.T("tui.editor.in_sync"))
				return e, nil
			}
			slug := e.name
			return e, func() tea.Msg { return OpenReconcileMsg{Feature: slug} }
		case "s":
			if !e.dirty() {
				e.status = i18n.T("tui.status.nothing")
				return e, nil
			}
			return e, e.enterBusy(i18n.T("tui.status.applying"), e.applyCmd())
		case "u":
			return e, e.enterBusy(i18n.T("tui.status.rebuilding"), e.upCmd())
		}
	}

	var cmd tea.Cmd
	e.table, cmd = e.table.Update(msg)
	return e, cmd
}

// toggleRemove flips removal staging on the selected row.
func (e *EditorModel) toggleRemove(prune bool) {
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
func (e *EditorModel) syncTable() {
	tr := make([]table.Row, 0, len(e.rows))
	for _, r := range e.rows {
		tr = append(tr, e.tableRow(r))
	}
	e.table.SetRows(tr)
}

func (e *EditorModel) openAddForm() {
	e.addVals = addVals{}
	e.form = newAddModeForm(e.ctx.Pd.Names(), &e.addVals).WithWidth(min(72, e.width-2))
	e.formMode = formAddMode
}

// fetchBranchesCmd refreshes a repo's remote refs then lists its branches, so the
// existing-branch selection and new-branch validation use refreshed refs.
func (e *EditorModel) fetchBranchesCmd(repo string) tea.Cmd {
	base := e.ctx.Cfg.BaseRepo(repo)
	return func() tea.Msg {
		gitx.Fetch(base)
		return editorBranchesMsg{repo: repo, branches: repos.Branches(base)}
	}
}

func (e *EditorModel) openMetaForm() {
	e.metaVals = metaVals{name: e.active, desc: e.desc}
	e.form = newMetaForm(&e.metaVals).WithWidth(min(72, e.width-2))
	e.formMode = formMeta
}

// applyRename persists a new active_name locally (keyed by the feature UUID). It
// changes only the display label — never the slug, manifest filename, or branches.
func (e *EditorModel) applyRename(newName string) {
	if newName == "" || e.man.ID == "" || newName == e.active {
		return
	}
	st, err := config.LoadState(e.ctx.Cfg.StateFile)
	if err != nil {
		return
	}
	st.EnsureFeature(e.man.ID, e.name)
	f := st.Features[e.man.ID]
	f.Name = newName
	st.Features[e.man.ID] = f
	if config.SaveState(e.ctx.Cfg.StateFile, st) == nil {
		e.active = newName
	}
}

// onFormDone reads back the completed form and applies the result — or advances to
// the next phase. It returns any follow-up command (a phase-2 form's Init, or the
// branch fetch for "from existing").
func (e *EditorModel) onFormDone() tea.Cmd {
	switch e.formMode {
	case formAddMode:
		// Both modes refresh refs before selecting or validating a branch.
		e.form = nil
		e.formMode = formNone
		return e.enterBusy(i18n.T("tui.status.fetching_branches"), e.fetchBranchesCmd(e.addVals.repo))
	case formAdd:
		sub := strings.TrimSpace(e.addVals.sub)
		e.stageAdd(superfeature.AddSpec{
			Repo:              e.addVals.repo,
			Sub:               sub,
			From:              strings.TrimSpace(e.addVals.from),
			OmitFeaturePrefix: e.addVals.omitPrefix,
		})
	case formAddExisting:
		// The chosen branch is attached directly without a feature prefix.
		if branch := strings.TrimSpace(e.addVals.branch); branch != "" {
			e.stageAdd(superfeature.AddSpec{Repo: e.addVals.repo, Sub: branch, ExistingBranch: true})
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
func (e *EditorModel) stageAdd(add superfeature.AddSpec) {
	if err := e.validateAdd(add); err != nil {
		e.status = components.ErrStyle.Render(err.Error())
		return
	}
	e.rows = append(e.rows, editorRow{
		kind:      rowStagedAdd,
		add:       add,
		addBranch: superfeature.ResolveBranchWith(e.man.BranchPrefix(), add.Sub, add.OmitFeaturePrefix || add.ExistingBranch),
	})
	e.syncTable()
	e.status = i18n.T("tui.status.staged_add")
}

// validateAdd also checks additions queued in this editor but not yet applied.
func (e *EditorModel) validateAdd(add superfeature.AddSpec) error {
	if err := superfeature.ValidateAdd(e.ctx.Cfg, e.man, add); err != nil {
		return err
	}
	branch := superfeature.ResolveBranchWith(e.man.BranchPrefix(), add.Sub, add.OmitFeaturePrefix || add.ExistingBranch)
	for _, row := range e.rows {
		if row.kind == rowStagedAdd && row.add.Repo == add.Repo && row.addBranch == branch {
			return i18n.Err("err.worktree_branch_exists", add.Repo, branch)
		}
	}
	return nil
}

// applyCmd runs the staged delta off the event loop.
func (e *EditorModel) applyCmd() tea.Cmd {
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
	cfg, pd := e.ctx.Cfg, e.ctx.Pd
	return func() tea.Msg {
		res, err := superfeature.ApplyEdit(cfg, pd, name, desc, adds, removes)
		return applyDoneMsg{res: res, err: err}
	}
}

func (e *EditorModel) upCmd() tea.Cmd {
	cfg, name := e.ctx.Cfg, e.name
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
func (e *EditorModel) reloadAfterApply(res *superfeature.EditResult, applyErr error) {
	man, err := manifest.Load(e.ctx.Cfg.ManifestPath(e.name))
	if err != nil {
		e.status = components.ErrStyle.Render(i18n.T("tui.status.reload_failed", err.Error()))
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
		e.status = components.ErrStyle.Render(i18n.T("tui.status.apply_partial", applyErr.Error()))
		return
	}
	added, removed := 0, 0
	if res != nil {
		added, removed = len(res.Added), len(res.Removed)
	}
	e.status = components.OkStyle.Render(i18n.T("tui.status.saved", added, removed))
}

func (e *EditorModel) View() string {
	if e.busy {
		// A dedicated loading screen so it's clear an async op is running (rather
		// than leaving the prior screen up with a small status line).
		msg := e.status
		if strings.TrimSpace(msg) == "" {
			msg = i18n.T("tui.editor.working")
		}
		body := e.spinner.View() + "  " + msg
		return components.DocStyle.Render(components.TitleStyle.Render(i18n.T("tui.editor.loading")) + "\n\n" + body + "\n\n" + components.HelpStyle.Render(i18n.T("tui.editor.loading_hint")))
	}

	if e.form != nil {
		title := i18n.T("tui.title.add_worktree")
		if e.formMode == formMeta {
			title = i18n.T("tui.title.edit_desc")
		}
		return components.DocStyle.Render(components.TitleStyle.Render(title) + "\n\n" + e.form.View() + "\n" + components.HelpStyle.Render(i18n.T("tui.form_help")))
	}

	var b strings.Builder
	header := fmt.Sprintf("%s  %s", components.TitleStyle.Render(e.active), components.DimStyle.Render(orDash(e.desc)))
	b.WriteString(header + "\n\n")
	b.WriteString(e.table.View() + "\n\n")

	if e.status != "" {
		b.WriteString(e.status + "\n")
	}
	if e.desync != nil {
		b.WriteString(components.WarnStyle.Render(i18n.T("tui.editor.desync", len(e.desync.Orphans), len(e.desync.Missing))) + "\n")
	}
	dirtyHint := ""
	if e.dirty() {
		dirtyHint = components.WarnStyle.Render(i18n.T("tui.unsaved"))
	}
	b.WriteString(components.HelpStyle.Render(i18n.T("tui.help") + dirtyHint))
	return components.DocStyle.Render(b.String())
}

func orDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "—"
	}
	return s
}
