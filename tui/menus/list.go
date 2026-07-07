package menus

import (
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/JoshuaLM114/workwood/config"
	"github.com/JoshuaLM114/workwood/i18n"
	"github.com/JoshuaLM114/workwood/superfeature"
	"github.com/JoshuaLM114/workwood/tui/components"
)

// featureItem is one row in the super-features page. The synthetic create row
// sorts to the top so "new feature" is always one keystroke away. name is the
// editable active_name (shown/filtered); slug is the immutable handle used to
// open the feature.
type featureItem struct {
	name   string
	slug   string
	desc   string
	count  int
	create bool
}

func (i featureItem) Title() string {
	if i.create {
		return i18n.T("tui.create_new")
	}
	return i.name
}

func (i featureItem) Description() string {
	if i.create {
		return i18n.T("tui.create_desc")
	}
	base := i18n.T("tui.worktree_count", i.count)
	if i.desc == "" {
		return base
	}
	return base + " · " + i.desc
}

func (i featureItem) FilterValue() string { return i.name }

// FeaturesModel wraps the bubbles list of super-features (its own page, reached
// from the root menu).
type FeaturesModel struct {
	list   list.Model
	status string // a transient notice (e.g. why create is blocked)
}

// NewFeatures builds the super-features picker from the loaded manifests.
func NewFeatures(ctx Ctx) *FeaturesModel {
	items := []list.Item{featureItem{create: true}}
	feats, _ := superfeature.List(ctx.Cfg) // a load error surfaces elsewhere; show what we can
	st, _ := config.LoadState(ctx.Cfg.StateFile)
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
	l.Title = i18n.T("tui.list_title", ctx.Cfg.ProjectName)
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(true)
	l.SetShowHelp(false) // the library help is English; we render a localized footer
	l.FilterInput.Prompt = i18n.T("tui.list.filter_prompt")
	l.Styles.Title = components.TitleStyle
	return &FeaturesModel{list: l}
}

// SetSize resizes the underlying list.
func (lm *FeaturesModel) SetSize(w, h int) { lm.list.SetSize(w, h) }

// SetStatus sets the transient notice line (e.g. why create is blocked).
func (lm *FeaturesModel) SetStatus(s string) { lm.status = s }

// FilterState reports the list's filter state (root checks it before stealing keys).
func (lm *FeaturesModel) FilterState() list.FilterState { return lm.list.FilterState() }

func (lm *FeaturesModel) Update(msg tea.Msg) tea.Cmd {
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
				return nil
			}
			if it.create {
				return func() tea.Msg { return OpenCreateMsg{} }
			}
			slug := it.slug
			return func() tea.Msg { return OpenEditorMsg{Feature: slug} }
		case "x": // delete the selected super-feature (guided, irreversible)
			it, ok := lm.list.SelectedItem().(featureItem)
			if !ok || it.create {
				return nil
			}
			slug, name := it.slug, it.name
			return func() tea.Msg { return OpenDeleteMsg{Feature: slug, Name: name} }
		}
	}
	var cmd tea.Cmd
	lm.list, cmd = lm.list.Update(msg)
	return cmd
}

func (lm *FeaturesModel) View() string {
	help := i18n.T("tui.list.help")
	if lm.list.FilterState() == list.Filtering {
		help = i18n.T("tui.list.help_filter")
	}
	out := lm.list.View()
	if lm.status != "" {
		out += "\n" + lm.status
	}
	return out + "\n" + components.HelpStyle.Render(help)
}
