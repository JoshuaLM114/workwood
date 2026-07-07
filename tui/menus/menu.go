package menus

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/JoshuaLM114/workwood/config"
	"github.com/JoshuaLM114/workwood/i18n"
	"github.com/JoshuaLM114/workwood/tui/components"
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

// MenuModel is the root navigation menu. It owns the boot notices (sync warning +
// feature back-link state) and can repair flagged links in place.
type MenuModel struct {
	ctx     Ctx
	notices Notices
	cursor  int
}

// NewMenu builds the root menu from the shared ctx and the on-boot notices.
func NewMenu(ctx Ctx, n Notices) MenuModel {
	return MenuModel{ctx: ctx, notices: n}
}

// SetNotices replaces the menu's boot notices (the root calls this when the async
// boot health check lands after the menu already exists).
func (mm *MenuModel) SetNotices(n Notices) { mm.notices = n }

func (mm MenuModel) Update(msg tea.Msg) (MenuModel, tea.Cmd) {
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
				return mm, func() tea.Msg { return OpenFeaturesMsg{} }
			case menuProject:
				return mm, func() tea.Msg { return OpenReposMsg{} }
			case menuSettings:
				return mm, func() tea.Msg { return OpenSettingsMsg{} }
			}
		case "r":
			// Repair any feature back-links flagged broken by the boot check.
			if len(mm.notices.BrokenLinks) > 0 {
				fixed := 0
				for _, slug := range mm.notices.BrokenLinks {
					if _, err := config.EnsureFeatureLink(mm.ctx.Cfg, slug); err == nil {
						fixed++
					}
				}
				mm.notices.BrokenLinks = nil
				mm.notices.LinkNotice = components.OkStyle.Render(i18n.T("tui.menu.links_fixed", fixed))
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

func (mm MenuModel) View() string {
	var b strings.Builder
	b.WriteString(components.TitleStyle.Render(i18n.T("tui.menu.title", mm.ctx.Cfg.ProjectName)) + "\n\n")
	for i, c := range menuOrder {
		title, desc := menuLabels(c)
		if i == mm.cursor {
			b.WriteString(components.SelectedRowStyle.Render("› "+title) + "  " + components.DimStyle.Render(desc) + "\n")
		} else {
			b.WriteString("  " + title + "  " + components.DimStyle.Render(desc) + "\n")
		}
	}
	if mm.notices.SyncWarning != "" {
		b.WriteString("\n" + components.WarnStyle.Render(mm.notices.SyncWarning) + "\n")
	}
	if mm.notices.LinkNotice != "" {
		b.WriteString("\n" + mm.notices.LinkNotice + "\n") // already styled (warn or ok)
	}
	b.WriteString("\n" + components.HelpStyle.Render(i18n.T("tui.menu.help")))
	return components.DocStyle.Render(b.String())
}
