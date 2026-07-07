package menus

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"

	"github.com/JoshuaLM114/workwood/config"
	"github.com/JoshuaLM114/workwood/i18n"
	"github.com/JoshuaLM114/workwood/models"
	"github.com/JoshuaLM114/workwood/tui/components"
)

// SettingsModel drives the dotfile-settings form, then writes the registry and
// applies the chosen language immediately. esc cancels without saving (back to the
// menu).
type SettingsModel struct {
	ctx   Ctx
	form  *huh.Form
	vals  settingsVals
	app   *models.AppSettings
	state *models.ProjectState
	home  string
}

// NewSettings loads the global app settings + project state and builds the form.
func NewSettings(ctx Ctx) (*SettingsModel, error) {
	app, home, err := config.LoadApp()
	if err != nil {
		return nil, err
	}
	st, err := config.LoadState(ctx.Cfg.StateFile)
	if err != nil {
		return nil, err
	}
	s := &SettingsModel{ctx: ctx, app: app, state: st, home: home}
	s.vals = settingsVals{
		lang:        i18n.Lang(),
		updateCheck: app.UpdateCheckEnabled(),
		name:        ctx.Cfg.ProjectName,
	}
	s.form = newSettingsForm(i18n.Supported, &s.vals).WithWidth(min(72, ctx.Width-4))
	return s, nil
}

// Init starts the form.
func (s *SettingsModel) Init() tea.Cmd { return s.form.Init() }

func (s *SettingsModel) Update(msg tea.Msg) (*SettingsModel, tea.Cmd) {
	if k, ok := msg.(tea.KeyMsg); ok {
		switch k.String() {
		case "ctrl+c":
			return s, tea.Quit
		case "esc": // cancel — back to the menu without saving
			return s, func() tea.Msg { return BackMsg{} }
		}
	}
	fm, cmd := s.form.Update(msg)
	if f, ok := fm.(*huh.Form); ok {
		s.form = f
	}
	switch s.form.State {
	case huh.StateCompleted:
		// Global app settings (language, update-check).
		app := s.app
		app.Language = s.vals.lang
		uc := s.vals.updateCheck
		app.UpdateCheck = &uc
		if err := config.SaveApp(s.home, app); err != nil {
			return s, func() tea.Msg { return ErrMsg{Err: err} }
		}
		// Project-local state (the active_name; checkout paths aren't editable).
		st := s.state
		st.Project = s.ctx.Cfg.ProjectID
		st.Name = s.vals.name
		if err := config.SaveState(s.ctx.Cfg.StateFile, st); err != nil {
			return s, func() tea.Msg { return ErrMsg{Err: err} }
		}
		// Apply immediately: switch language + update the display name.
		i18n.Init(s.vals.lang)
		s.ctx.Cfg.ProjectName = s.vals.name
		return s, func() tea.Msg { return BackMsg{} }
	case huh.StateAborted:
		return s, func() tea.Msg { return BackMsg{} }
	}
	return s, cmd
}

func (s *SettingsModel) View() string {
	return components.DocStyle.Render(components.TitleStyle.Render(i18n.T("tui.settings.title")) + "\n\n" + s.form.View() + "\n" + components.HelpStyle.Render(i18n.T("tui.form_help")))
}
