package tui

import (
	"strings"

	"github.com/JoshuaLM114/workwood/action"
	"github.com/JoshuaLM114/workwood/config"
	"github.com/JoshuaLM114/workwood/i18n"
	"github.com/JoshuaLM114/workwood/manifest"
	"github.com/JoshuaLM114/workwood/targetcfg"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
)

var selectedRowStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("231")).Background(lipgloss.Color("63"))

type actionsFormMode int

const (
	afNone actionsFormMode = iota
	afAddPath
	afRenameKey
	afSavePreset
	afConfirmOverwrite
	afLoadPreset
	afChooseAction
)

// actionsModel is the per-feature Actions screen: a toggleable tree of candidate
// targets (reference repos, worktrees, and .workwood/targets.yml services), with
// add-path / rename / save+load presets, and running an action against the enabled
// set (the "working set", persisted in workwood-state.yml).
type actionsModel struct {
	m    *Model
	slug string
	man  *manifest.Manifest

	working   targetcfg.Set
	nodes     []targetcfg.Node
	visible   []int        // node indices shown given collapse state
	collapsed map[int]bool // serviceParent node index → collapsed
	cursor    int          // index into visible

	actions     []action.Action // discovered (marked + executable) actions
	needChmod   []string        // marked files that aren't executable (common mistake)
	selected    string          // the chosen action's name (the top "dropdown")
	targetValid map[string]bool // per-target Validate result (path → passed); nil = not validated

	form          *huh.Form
	formMode      actionsFormMode
	pathVals      pathVals
	keyVals       keyVals
	nameVals      nameVals
	confirmVals   confirmVals
	pendingPreset string // preset name awaiting an overwrite confirm
	selVals       selectVals
	lastAction    string

	status string
	width  int
	height int
}

type openActionsMsg struct{ feature string }
type actionDoneMsg struct{ err error }

func newActionsModel(m *Model, slug string) (*actionsModel, error) {
	man, err := manifest.Load(m.cfg.ManifestPath(slug))
	if err != nil {
		return nil, err
	}
	working, err := targetcfg.Working(m.cfg, m.pd, man)
	if err != nil {
		return nil, err
	}
	a := &actionsModel{m: m, slug: slug, man: man, working: working, collapsed: map[int]bool{}}
	// Drop stale managed paths (e.g. a removed worktree) and persist if changed.
	if removed := targetcfg.Prune(m.cfg, working); len(removed) > 0 {
		a.save()
	}
	a.refreshActions()
	a.rebuild()
	a.validateSelected() // validate per target on entering the screen
	return a, nil
}

// refreshActions re-scans workwood/actions for marked actions, keeping the current
// selection if it's still present (else selecting the first, or none).
func (a *actionsModel) refreshActions() {
	a.actions, a.needChmod = action.Scan(a.m.cfg)
	a.targetValid = nil // re-scanned → re-validate on next V
	for _, act := range a.actions {
		if act.Name == a.selected {
			return // still valid
		}
	}
	if len(a.actions) > 0 {
		a.selected = a.actions[0].Name
	} else {
		a.selected = ""
	}
}

// selectedDesc returns the marker label of the currently-selected action.
func (a *actionsModel) selectedDesc() string {
	for _, act := range a.actions {
		if act.Name == a.selected {
			return act.Description
		}
	}
	return ""
}

func (a *actionsModel) rebuild() {
	a.nodes = targetcfg.Candidates(a.m.cfg, a.m.pd, a.man, a.working)
	a.visible = nil
	for i, n := range a.nodes {
		if n.Parent >= 0 && a.collapsed[n.Parent] {
			continue
		}
		a.visible = append(a.visible, i)
	}
	if a.cursor >= len(a.visible) {
		a.cursor = len(a.visible) - 1
	}
	if a.cursor < 0 {
		a.cursor = 0
	}
}

func (a *actionsModel) cur() (targetcfg.Node, int, bool) {
	if a.cursor < 0 || a.cursor >= len(a.visible) {
		return targetcfg.Node{}, -1, false
	}
	return a.nodes[a.visible[a.cursor]], a.visible[a.cursor], true
}

func (a *actionsModel) save() {
	// Note: per-target results are independent of which targets are toggled (each
	// is validated with its own single-target context), so toggling does NOT
	// invalidate them — only changing the action or re-scanning does.
	_ = targetcfg.SaveWorking(a.m.cfg, a.man, a.working)
}

// failedEnabled returns the keys of ENABLED targets that failed validation — the
// ones that would block a run.
func (a *actionsModel) failedEnabled() []string {
	var bad []string
	for _, n := range a.nodes {
		if !n.Toggled {
			continue
		}
		if ok, checked := a.targetValid[n.Path]; checked && !ok {
			bad = append(bad, n.Key)
		}
	}
	return bad
}

// findAction returns the discovered Action for name, or nil.
func (a *actionsModel) findAction(name string) *action.Action {
	for i := range a.actions {
		if a.actions[i].Name == name {
			return &a.actions[i]
		}
	}
	return nil
}

// validateSelected validates the selected action against EVERY available target
// (each candidate node, not just the enabled ones) by running the action's
// Validate with that one target as its context. The per-target pass/fail is shown
// as a ✓/✗ next to each target in the tree. A structurally-incomplete action
// (missing Run/Validate) is reported without running anything.
func (a *actionsModel) validateSelected() {
	a.targetValid = nil
	act := a.findAction(a.selected)
	if act == nil {
		return
	}
	if !act.Runnable() {
		a.status = errStyle.Render(i18n.T("tui.actions.unavailable_status", a.selected, i18n.T("tui.actions.missing_methods")))
		return
	}
	res := map[string]bool{}
	pass, total := 0, 0
	for _, n := range a.nodes {
		if !n.Toggleable() {
			continue
		}
		total++
		ok := action.Validate(a.m.cfg, a.slug, a.selected, map[string]string{n.Key: n.Path}, a.man.Vars) == nil
		res[n.Path] = ok
		if ok {
			pass++
		}
	}
	a.targetValid = res
	a.status = okStyle.Render(i18n.T("tui.actions.validated", a.selected, pass, total))
}

func (a *actionsModel) setSize(w, h int) { a.width, a.height = w, h }

func (a *actionsModel) Update(msg tea.Msg) (*actionsModel, tea.Cmd) {
	if a.form != nil {
		if k, ok := msg.(tea.KeyMsg); ok {
			switch k.String() {
			case "esc":
				a.form = nil
				a.formMode = afNone
				a.status = i18n.T("tui.status.cancelled")
				return a, nil
			case "ctrl+c":
				return a, tea.Quit
			}
		}
		fm, cmd := a.form.Update(msg)
		if f, ok := fm.(*huh.Form); ok {
			a.form = f
		}
		if a.form.State == huh.StateCompleted {
			return a, a.onFormDone()
		} else if a.form.State == huh.StateAborted {
			a.form = nil
			a.formMode = afNone
		}
		return a, cmd
	}

	switch msg := msg.(type) {
	case actionDoneMsg:
		if msg.err != nil {
			a.status = errStyle.Render(i18n.T("tui.actions.run_failed", msg.err.Error()))
		} else {
			a.status = okStyle.Render(i18n.T("tui.actions.ran", a.lastAction))
		}
		a.rebuild()
		return a, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return a, tea.Quit
		case "esc":
			return a, func() tea.Msg { return backMsg{} }
		case "up", "k":
			if a.cursor > 0 {
				a.cursor--
			}
		case "down", "j":
			if a.cursor < len(a.visible)-1 {
				a.cursor++
			}
		case " ":
			a.toggle()
		case "enter":
			a.expandCollapse()
		case "p":
			a.pathVals = pathVals{}
			a.form = newAddPathForm(&a.pathVals).WithWidth(min(72, a.width-4))
			a.formMode = afAddPath
			return a, a.form.Init()
		case "r":
			if n, _, ok := a.cur(); ok && n.Toggled {
				a.keyVals = keyVals{key: n.Key}
				a.form = newRenameKeyForm(&a.keyVals).WithWidth(min(72, a.width-4))
				a.formMode = afRenameKey
				return a, a.form.Init()
			}
			a.status = i18n.T("tui.actions.toggle_first")
		case "S":
			a.nameVals = nameVals{name: a.lastPreset()} // jump to the last-loaded preset
			a.form = newSavePresetForm(targetcfg.ListPresets(a.m.cfg), &a.nameVals).WithWidth(min(72, a.width-4))
			a.formMode = afSavePreset
			return a, a.form.Init()
		case "g":
			a.regeneratePreset()
		case "L":
			presets := targetcfg.ListPresets(a.m.cfg)
			if len(presets) == 0 {
				a.status = i18n.T("tui.actions.no_presets")
				return a, nil
			}
			a.selVals = selectVals{}
			a.form = newSelectForm(i18n.T("tui.title.load_preset"), presets, &a.selVals).WithWidth(min(72, a.width-4))
			a.formMode = afLoadPreset
			return a, a.form.Init()
		case "d":
			if len(a.actions) == 0 {
				a.status = warnStyle.Render(i18n.T("tui.actions.none_found", a.m.cfg.ActionsDir))
				return a, nil
			}
			opts := make([]huh.Option[string], 0, len(a.actions))
			for _, act := range a.actions {
				label := act.Name
				if !act.Runnable() { // structurally unavailable → mark it in the picker
					label += "  " + i18n.T("tui.actions.picker_unavailable")
				}
				opts = append(opts, huh.NewOption(label, act.Name))
			}
			a.selVals = selectVals{choice: a.selected}
			a.form = newSelectOptForm(i18n.T("tui.title.choose_action"), opts, &a.selVals).WithWidth(min(72, a.width-4))
			a.formMode = afChooseAction
			return a, a.form.Init()
		case "V":
			if a.selected == "" {
				a.status = warnStyle.Render(i18n.T("tui.actions.none_found", a.m.cfg.ActionsDir))
				return a, nil
			}
			a.validateSelected()
		case "R":
			if a.selected == "" {
				a.status = warnStyle.Render(i18n.T("tui.actions.none_found", a.m.cfg.ActionsDir))
				return a, nil
			}
			// Don't run a structurally-incomplete action (missing Run/Validate).
			if act := a.findAction(a.selected); act == nil || !act.Runnable() {
				a.status = errStyle.Render(i18n.T("tui.actions.cant_run", a.selected))
				return a, nil
			}
			// Don't run when an enabled target failed validation.
			if a.targetValid == nil {
				a.validateSelected()
			}
			if failed := a.failedEnabled(); len(failed) > 0 {
				a.status = errStyle.Render(i18n.T("tui.actions.blocked_failed", strings.Join(failed, ", ")))
				return a, nil
			}
			return a, a.runAction(a.selected)
		case "f":
			a.refreshActions()
			a.status = okStyle.Render(i18n.T("tui.actions.refreshed", len(a.actions)))
		}
	}
	return a, nil
}

func (a *actionsModel) toggle() {
	n, _, ok := a.cur()
	if !ok || !n.Toggleable() {
		return
	}
	if n.Toggled {
		targetcfg.Disable(a.working, n.Path)
	} else {
		targetcfg.Enable(a.working, n.Key, n.Path)
	}
	a.save()
	a.rebuild()
}

func (a *actionsModel) expandCollapse() {
	n, idx, ok := a.cur()
	if !ok || n.Kind != targetcfg.KindServiceParent {
		return
	}
	a.collapsed[idx] = !a.collapsed[idx]
	a.rebuild()
}

func (a *actionsModel) onFormDone() tea.Cmd {
	mode := a.formMode
	a.form = nil
	a.formMode = afNone
	switch mode {
	case afAddPath:
		a.addPath()
	case afRenameKey:
		a.renameKey()
	case afSavePreset:
		name := strings.TrimSpace(a.nameVals.name)
		if name == "" {
			return nil
		}
		// Overwriting an existing preset asks first; a new name saves straight away.
		if targetcfg.PresetExists(a.m.cfg, name) {
			a.pendingPreset = name
			a.confirmVals = confirmVals{}
			a.form = newConfirmForm(i18n.T("tui.actions.overwrite_confirm", name), &a.confirmVals).WithWidth(min(72, a.width-4))
			a.formMode = afConfirmOverwrite
			return a.form.Init()
		}
		a.savePreset(name)
	case afConfirmOverwrite:
		if a.confirmVals.ok {
			a.savePreset(a.pendingPreset)
		} else {
			a.status = i18n.T("tui.status.cancelled")
		}
		a.pendingPreset = ""
	case afLoadPreset:
		a.loadPreset(a.selVals.choice)
	case afChooseAction:
		if a.selVals.choice != a.selected {
			a.selected = a.selVals.choice
			a.validateSelected() // re-validate per target on action change
		}
	}
	return nil
}

func (a *actionsModel) addPath() {
	p := strings.TrimSpace(a.pathVals.path)
	if p == "" {
		return
	}
	key, abs, _ := targetcfg.AddPath(p)
	if uk := strings.TrimSpace(a.pathVals.key); uk != "" {
		key = uk
	}
	if key == "" {
		a.status = errStyle.Render(i18n.T("tui.actions.need_key"))
		return
	}
	targetcfg.Enable(a.working, key, abs)
	a.save()
	a.rebuild()
	a.status = okStyle.Render(i18n.T("tui.actions.added_path", key))
}

func (a *actionsModel) renameKey() {
	n, _, ok := a.cur()
	if !ok || !n.Toggled {
		return
	}
	newKey := strings.TrimSpace(a.keyVals.key)
	if newKey == "" || newKey == n.Key {
		return
	}
	targetcfg.Rename(a.working, n.Key, newKey)
	a.save()
	a.rebuild()
	a.status = okStyle.Render(i18n.T("tui.actions.renamed", newKey))
}

func (a *actionsModel) savePreset(name string) {
	if err := targetcfg.SavePreset(a.m.cfg, name, a.working); err != nil {
		a.status = errStyle.Render(i18n.T("tui.actions.run_failed", err.Error()))
		return
	}
	a.recordLastPreset(name)
	a.status = okStyle.Render(i18n.T("tui.actions.saved_preset", name))
}

func (a *actionsModel) loadPreset(name string) {
	set, err := targetcfg.LoadPreset(a.m.cfg, name)
	if err != nil {
		a.status = errStyle.Render(i18n.T("tui.actions.run_failed", err.Error()))
		return
	}
	a.working = set
	a.save()
	a.recordLastPreset(name)
	a.rebuild()
	a.status = okStyle.Render(i18n.T("tui.actions.loaded_preset", name))
}

// regeneratePreset rewrites <feature>.yml from a fresh CleanSet (repos + current
// worktrees), overwriting it — the explicit "pick up new worktrees" action.
func (a *actionsModel) regeneratePreset() {
	if err := targetcfg.SavePreset(a.m.cfg, a.slug, targetcfg.CleanSet(a.m.cfg, a.m.pd, a.man)); err != nil {
		a.status = errStyle.Render(i18n.T("tui.actions.run_failed", err.Error()))
		return
	}
	a.recordLastPreset(a.slug)
	a.status = okStyle.Render(i18n.T("tui.actions.regenerated", a.slug))
}

// lastPreset is the preset name last loaded/saved for this feature (UI memory).
func (a *actionsModel) lastPreset() string {
	st, err := config.LoadState(a.m.cfg.StateFile)
	if err != nil {
		return ""
	}
	return st.LastPreset(a.man.ID)
}

// recordLastPreset persists the last loaded/saved preset name for this feature.
func (a *actionsModel) recordLastPreset(name string) {
	st, err := config.LoadState(a.m.cfg.StateFile)
	if err != nil {
		return
	}
	st.SetLastPreset(a.man.ID, name)
	_ = config.SaveState(a.m.cfg.StateFile, st)
}

func (a *actionsModel) runAction(name string) tea.Cmd {
	cmd, err := action.Command(a.m.cfg, a.slug, name, a.working, a.man.Vars)
	if err != nil {
		a.status = errStyle.Render(i18n.T("tui.actions.run_failed", err.Error()))
		return nil
	}
	a.lastAction = name
	// Release the terminal so an action (e.g. tmux) can take it over, then resume.
	return tea.ExecProcess(cmd, func(err error) tea.Msg { return actionDoneMsg{err: err} })
}

func (a *actionsModel) View() string {
	if a.form != nil {
		title := a.formTitle()
		return docStyle.Render(titleStyle.Render(title) + "\n\n" + a.form.View() + "\n" + helpStyle.Render(i18n.T("tui.form_help")))
	}

	var b strings.Builder
	b.WriteString(titleStyle.Render(i18n.T("tui.actions.title", a.man.Feature)) + "\n\n")

	// Top panel: the action "dropdown" (selected action), or a no-actions notice.
	if a.selected == "" {
		b.WriteString(warnStyle.Render(i18n.T("tui.actions.none_found", a.m.cfg.ActionsDir)) + "\n")
		if len(a.needChmod) > 0 {
			b.WriteString(errStyle.Render(i18n.T("actions.need_chmod", strings.Join(a.needChmod, ", "))) + "\n")
		}
	} else {
		line := i18n.T("tui.actions.selected", a.selected)
		if d := a.selectedDesc(); d != "" {
			line += "  " + dimStyle.Render(d)
		}
		// Badge: structural invalidity (missing Run/Validate) always shows; once
		// validated, a per-target pass summary.
		if act := a.findAction(a.selected); act != nil && !act.Runnable() {
			line += "  " + errStyle.Render(i18n.T("tui.actions.badge_unavailable"))
		} else if a.targetValid != nil {
			pass := 0
			for _, ok := range a.targetValid {
				if ok {
					pass++
				}
			}
			line += "  " + dimStyle.Render(i18n.T("tui.actions.target_summary", pass, len(a.targetValid)))
		}
		b.WriteString(line + "\n")
	}
	sepW := a.width
	if sepW <= 0 || sepW > 64 {
		sepW = 64
	}
	b.WriteString(dimStyle.Render(strings.Repeat("─", sepW)) + "\n\n")

	// Bottom panel: the target tree.
	if len(a.visible) == 0 {
		b.WriteString(dimStyle.Render(i18n.T("tui.actions.tree_empty")) + "\n")
	}
	for vi, ni := range a.visible {
		n := a.nodes[ni]
		indent := strings.Repeat("  ", n.Depth)
		var marker string
		switch {
		case n.Kind == targetcfg.KindServiceParent:
			if a.collapsed[ni] {
				marker = "▸"
			} else {
				marker = "▾"
			}
		case n.Toggled:
			marker = "[x]"
		default:
			marker = "[ ]"
		}
		label := n.Key
		path := n.Path
		if !n.Exists {
			path += " " + warnStyle.Render(i18n.T("tui.actions.missing"))
		}
		// Per-target validation: a ✓/✗ mark on every validated target, and the
		// ENABLED ones get their text coloured by the result (green = passed,
		// red = failed).
		vmark := ""
		labelOut := label
		if ok, checked := a.targetValid[n.Path]; checked && n.Toggleable() {
			if ok {
				vmark = "  " + okStyle.Render("✓")
				if n.Toggled {
					labelOut = okStyle.Render(label)
				}
			} else {
				vmark = "  " + errStyle.Render("✗")
				if n.Toggled {
					labelOut = errStyle.Render(label)
				}
			}
		}
		line := indent + marker + " " + labelOut + "  " + dimStyle.Render(path)
		if vi == a.cursor {
			line = selectedRowStyle.Render(indent + marker + " " + label + "  " + path)
		}
		b.WriteString(line + vmark + "\n")
	}

	b.WriteString("\n")
	if a.status != "" {
		b.WriteString(a.status + "\n")
	}
	b.WriteString(helpStyle.Render(i18n.T("tui.actions.help")))
	return docStyle.Render(b.String())
}

func (a *actionsModel) formTitle() string {
	switch a.formMode {
	case afAddPath:
		return i18n.T("tui.title.add_path")
	case afRenameKey:
		return i18n.T("tui.title.rename_key")
	case afSavePreset:
		return i18n.T("tui.title.save_preset")
	case afConfirmOverwrite:
		return i18n.T("tui.title.save_preset")
	case afLoadPreset:
		return i18n.T("tui.title.load_preset")
	case afChooseAction:
		return i18n.T("tui.title.choose_action")
	}
	return ""
}
