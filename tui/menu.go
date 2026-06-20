package tui

import (
	"strings"

	"github.com/JoshuaLM114/workwood/i18n"
	tea "github.com/charmbracelet/bubbletea"
)

// menuChoice is a top-level menu entry.
type menuChoice int

const (
	menuFeatures menuChoice = iota // → the super-features page
	menuProject                    // → the project repo editor
	menuSettings                   // → the settings screen
)

// menuOrder fixes the menu order: super-features, then "edit project", then
// settings (edit-project sits directly above settings).
var menuOrder = []menuChoice{menuFeatures, menuProject, menuSettings}

// menuModel is the root navigation menu.
type menuModel struct{ cursor int }

func (mm menuModel) update(m *Model, msg tea.Msg) (menuModel, tea.Cmd) {
	if k, ok := msg.(tea.KeyMsg); ok {
		switch k.String() {
		case "up", "k":
			if mm.cursor > 0 {
				mm.cursor--
			}
		case "down", "j":
			if mm.cursor < len(menuOrder)-1 {
				mm.cursor++
			}
		case "enter":
			switch menuOrder[mm.cursor] {
			case menuFeatures:
				return mm, func() tea.Msg { return openFeaturesMsg{} }
			case menuProject:
				return mm, func() tea.Msg { return openReposMsg{} }
			case menuSettings:
				return mm, func() tea.Msg { return openSettingsMsg{} }
			}
		}
	}
	return mm, nil
}

// menuLabels returns the (title, description) for a menu entry.
func menuLabels(c menuChoice) (string, string) {
	switch c {
	case menuFeatures:
		return i18n.T("tui.menu.features"), i18n.T("tui.menu.features_desc")
	case menuProject:
		return i18n.T("tui.menu.project"), i18n.T("tui.menu.project_desc")
	default:
		return i18n.T("tui.settings.item_title"), i18n.T("tui.settings.item_desc")
	}
}

func (mm menuModel) View(m *Model) string {
	var b strings.Builder
	b.WriteString(titleStyle.Render(i18n.T("tui.menu.title", m.cfg.ProjectName)) + "\n\n")
	for i, c := range menuOrder {
		title, desc := menuLabels(c)
		if i == mm.cursor {
			b.WriteString(selectedRowStyle.Render("› "+title) + "  " + dimStyle.Render(desc) + "\n")
		} else {
			b.WriteString("  " + title + "  " + dimStyle.Render(desc) + "\n")
		}
	}
	b.WriteString("\n" + helpStyle.Render(i18n.T("tui.menu.help")))
	return docStyle.Render(b.String())
}
