package tui

import (
	"strings"

	"github.com/JoshuaLM114/workwood/i18n"
	"github.com/JoshuaLM114/workwood/manifest"
	"github.com/JoshuaLM114/workwood/superfeature"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
)

// reconcile{Orphan,Missing}Vals bind one drift item's chosen resolution.
type reconcileOrphanVals struct {
	o      superfeature.Orphan
	action string // adopt | remove | skip
}
type reconcileMissingVals struct {
	w      manifest.Worktree
	action string // rebuild | drop | skip
}
type reconcileVals struct {
	orphans []reconcileOrphanVals
	missing []reconcileMissingVals
}

// reconcileModel walks a feature's manifest↔disk drift one item per page, then
// applies the chosen fixes and shows a log. Opened from the editor with `D`.
type reconcileModel struct {
	m    *Model
	slug string
	form *huh.Form
	vals *reconcileVals
	done bool
	log  []string
}

// newReconcileModel re-diagnoses the feature and builds the walkthrough. Orphans
// default to Adopt (recover them) unless unidentifiable; missing default to Rebuild.
func newReconcileModel(m *Model, slug string) (*reconcileModel, error) {
	d, err := superfeature.Diagnose(m.cfg, m.pd, slug)
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
	r := &reconcileModel{m: m, slug: slug, vals: vals}
	r.form = newReconcileForm(vals).WithWidth(min(80, m.width-4))
	return r, nil
}

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

func (r *reconcileModel) Update(msg tea.Msg) (*reconcileModel, tea.Cmd) {
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
func (r *reconcileModel) execute() {
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
	for _, oc := range superfeature.Reconcile(r.m.cfg, r.slug, plan) {
		if oc.Err != nil {
			r.log = append(r.log, errStyle.Render(oc.Err.Error()))
		} else {
			r.log = append(r.log, oc.Msg)
		}
	}
	if len(r.log) == 0 {
		r.log = append(r.log, i18n.T("tui.reconcile.nothing"))
	}
}

// back rebuilds + shows the editor (reflecting adopted/rebuilt worktrees).
func (r *reconcileModel) back() tea.Msg { return openEditorMsg{feature: r.slug} }

func (r *reconcileModel) View() string {
	title := titleStyle.Render(i18n.T("tui.reconcile.title", r.slug))
	if r.done {
		body := okStyle.Render(i18n.T("tui.reconcile.done")) + "\n\n" + strings.Join(r.log, "\n")
		return docStyle.Render(title + "\n\n" + body + "\n\n" + helpStyle.Render(i18n.T("tui.reconcile.return")))
	}
	return docStyle.Render(title + "\n\n" + r.form.View() + "\n" + helpStyle.Render(i18n.T("tui.reconcile.help")))
}
