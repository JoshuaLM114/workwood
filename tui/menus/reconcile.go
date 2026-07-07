package menus

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"

	"github.com/JoshuaLM114/workwood/i18n"
	"github.com/JoshuaLM114/workwood/models"
	"github.com/JoshuaLM114/workwood/superfeature"
	"github.com/JoshuaLM114/workwood/tui/components"
)

// reconcile{Orphan,Missing}Vals bind one drift item's chosen resolution.
type reconcileOrphanVals struct {
	o      superfeature.Orphan
	action string // adopt | remove | skip
}
type reconcileMissingVals struct {
	w      models.Worktree
	action string // rebuild | drop | skip
}
type reconcileVals struct {
	orphans []reconcileOrphanVals
	missing []reconcileMissingVals
}

// ReconcileModel walks a feature's manifest↔disk drift one item per page, then
// applies the chosen fixes and shows a log. Opened from the editor with `D`.
type ReconcileModel struct {
	ctx  Ctx
	slug string
	form *huh.Form
	vals *reconcileVals
	done bool
	log  []string
}

// NewReconcile re-diagnoses the feature and builds the walkthrough. Orphans
// default to Adopt (recover them) unless unidentifiable; missing default to Rebuild.
func NewReconcile(ctx Ctx, slug string) (*ReconcileModel, error) {
	d, err := superfeature.Diagnose(ctx.Cfg, ctx.Pd, slug)
	if err != nil {
		return nil, err
	}
	vals := &reconcileVals{}
	for _, o := range d.Orphans {
		def := "adopt"
		if o.Repo == "" || o.Branch == "" {
			def = "remove" // can't adopt one we can't identify
		}
		vals.orphans = append(vals.orphans, reconcileOrphanVals{o: o, action: def})
	}
	for _, w := range d.Missing {
		vals.missing = append(vals.missing, reconcileMissingVals{w: w, action: "rebuild"})
	}
	r := &ReconcileModel{ctx: ctx, slug: slug, vals: vals}
	r.form = newReconcileForm(vals).WithWidth(min(80, ctx.Width-4))
	return r, nil
}

// Init starts the walkthrough form.
func (r *ReconcileModel) Init() tea.Cmd { return r.form.Init() }

func newReconcileForm(v *reconcileVals) *huh.Form {
	var groups []*huh.Group
	for i := range v.orphans {
		o := &v.orphans[i]
		opts := []huh.Option[string]{}
		if o.o.Repo != "" && o.o.Branch != "" {
			opts = append(opts, huh.NewOption(i18n.T("tui.reconcile.adopt"), "adopt"))
		}
		opts = append(opts,
			huh.NewOption(i18n.T("tui.reconcile.remove"), "remove"),
			huh.NewOption(i18n.T("tui.reconcile.skip"), "skip"),
		)
		groups = append(groups, huh.NewGroup(
			huh.NewNote().Title(i18n.T("tui.reconcile.orphan_title", orDash(o.o.Repo), orDash(o.o.Branch))).Description(o.o.Abs),
			huh.NewSelect[string]().Options(opts...).Value(&o.action),
		))
	}
	for i := range v.missing {
		w := &v.missing[i]
		groups = append(groups, huh.NewGroup(
			huh.NewNote().Title(i18n.T("tui.reconcile.missing_title", w.w.Repo, w.w.Branch)).Description(w.w.Path),
			huh.NewSelect[string]().Options(
				huh.NewOption(i18n.T("tui.reconcile.rebuild"), "rebuild"),
				huh.NewOption(i18n.T("tui.reconcile.drop"), "drop"),
				huh.NewOption(i18n.T("tui.reconcile.skip"), "skip"),
			).Value(&w.action),
		))
	}
	return form(groups...)
}

func (r *ReconcileModel) Update(msg tea.Msg) (*ReconcileModel, tea.Cmd) {
	if k, ok := msg.(tea.KeyMsg); ok {
		switch k.String() {
		case "ctrl+c":
			return r, tea.Quit
		case "esc":
			return r, r.back
		case "enter":
			if r.done {
				return r, r.back
			}
		}
	}
	if r.done {
		return r, nil
	}
	fm, cmd := r.form.Update(msg)
	if f, ok := fm.(*huh.Form); ok {
		r.form = f
	}
	switch r.form.State {
	case huh.StateCompleted:
		r.execute()
		r.done = true
		return r, nil
	case huh.StateAborted:
		return r, r.back
	}
	return r, cmd
}

// execute applies each chosen resolution, collecting a log.
func (r *ReconcileModel) execute() {
	var plan superfeature.ReconcilePlan
	for _, ov := range r.vals.orphans {
		switch ov.action {
		case "adopt":
			plan.AdoptOrphans = append(plan.AdoptOrphans, ov.o)
		case "remove":
			plan.RemoveOrphans = append(plan.RemoveOrphans, ov.o)
		}
	}
	for _, mv := range r.vals.missing {
		switch mv.action {
		case "rebuild":
			plan.RebuildMissing = append(plan.RebuildMissing, mv.w)
		case "drop":
			plan.DropMissing = append(plan.DropMissing, mv.w)
		}
	}
	for _, oc := range superfeature.Reconcile(r.ctx.Cfg, r.slug, plan) {
		if oc.Err != nil {
			r.log = append(r.log, components.ErrStyle.Render(oc.Err.Error()))
		} else {
			r.log = append(r.log, oc.Msg)
		}
	}
	if len(r.log) == 0 {
		r.log = append(r.log, i18n.T("tui.reconcile.nothing"))
	}
}

// back rebuilds + shows the editor (reflecting adopted/rebuilt worktrees).
func (r *ReconcileModel) back() tea.Msg { return OpenEditorMsg{Feature: r.slug} }

func (r *ReconcileModel) View() string {
	title := components.TitleStyle.Render(i18n.T("tui.reconcile.title", r.slug))
	if r.done {
		body := components.OkStyle.Render(i18n.T("tui.reconcile.done")) + "\n\n" + strings.Join(r.log, "\n")
		return components.DocStyle.Render(title + "\n\n" + body + "\n\n" + components.HelpStyle.Render(i18n.T("tui.reconcile.return")))
	}
	return components.DocStyle.Render(title + "\n\n" + r.form.View() + "\n" + components.HelpStyle.Render(i18n.T("tui.reconcile.help")))
}
