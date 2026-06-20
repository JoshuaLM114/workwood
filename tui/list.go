package tui

import (
	"github.com/JoshuaLM114/workwood/config"
	"github.com/JoshuaLM114/workwood/i18n"
	"github.com/JoshuaLM114/workwood/superfeature"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
)

// featureItem is one row in the feature picker. The synthetic create row sorts
// to the top so "new feature" is always one keystroke away. name is the editable
// active_name (shown/filtered); slug is the immutable handle used to open the
// feature.
type featureItem struct {
	name     string
	slug     string
	desc     string
	count    int
	create   bool
	settings bool
}

func (i featureItem) Title() string {
	switch {
	case i.create:
		return i18n.T("tui.create_new")
	case i.settings:
		return i18n.T("tui.settings.item_title")
	}
	return i.name
}

func (i featureItem) Description() string {
	switch {
	case i.create:
		return i18n.T("tui.create_desc")
	case i.settings:
		return i18n.T("tui.settings.item_desc")
	}
	base := i18n.T("tui.worktree_count", i.count)
	if i.desc == "" {
		return base
	}
	return base + " · " + i.desc
}

func (i featureItem) FilterValue() string { return i.name }

// listModel wraps the bubbles list of super-features.
type listModel struct {
	list list.Model
}

// newListModel builds the picker from the loaded manifests.
func newListModel(m *Model) listModel {
	items := []list.Item{featureItem{create: true}, featureItem{settings: true}}
	feats, _ := superfeature.List(m.cfg) // a load error surfaces elsewhere; show what we can
	st, _ := config.LoadState(m.cfg.StateFile)
	for _, f := range feats {
		name := f.Feature
		if st != nil {
			if fs, ok := st.FeatureByUUID(f.ID); ok {
				name = fs.DisplayName()
			}
		}
		items = append(items, featureItem{name: name, slug: f.Feature, desc: f.Description, count: len(f.Worktrees)})
	}
	delegate := list.NewDefaultDelegate()
	l := list.New(items, delegate, 0, 0)
	l.Title = i18n.T("tui.list_title", m.cfg.ProjectName)
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(true)
	l.SetShowHelp(false) // the library help is English; we render a localized footer
	l.FilterInput.Prompt = i18n.T("tui.list.filter_prompt")
	l.Styles.Title = titleStyle
	return listModel{list: l}
}

func (lm listModel) update(m *Model, msg tea.Msg) (listModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		// Don't steal keys while the list's own filter input is focused.
		if lm.list.FilterState() == list.Filtering {
			break
		}
		switch msg.String() {
		case "enter":
			it, ok := lm.list.SelectedItem().(featureItem)
			if !ok {
				return lm, nil
			}
			if it.create {
				return lm, func() tea.Msg { return openCreateMsg{} }
			}
			if it.settings {
				return lm, func() tea.Msg { return openSettingsMsg{} }
			}
			slug := it.slug
			return lm, func() tea.Msg { return openEditorMsg{feature: slug} }
		}
	}
	var cmd tea.Cmd
	lm.list, cmd = lm.list.Update(msg)
	return lm, cmd
}

func (lm listModel) View() string {
	help := i18n.T("tui.list.help")
	if lm.list.FilterState() == list.Filtering {
		help = i18n.T("tui.list.help_filter")
	}
	return lm.list.View() + "\n" + helpStyle.Render(help)
}
