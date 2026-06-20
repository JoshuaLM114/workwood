// Package tui is the Bubble Tea front-end. It opens a root menu (Super-features ·
// Edit project · Settings); the super-features page drops into a malleable editor
// where worktrees can be staged for addition/removal and applied as one delta, and
// "Edit project" manages the base repos in workwood.yml. Forms use huh.
//
// The TUI operates on ONE already-resolved project (chosen via -p or cwd).
package tui

import (
	"os"

	"github.com/JoshuaLM114/workwood/config"
	"github.com/JoshuaLM114/workwood/i18n"
	"github.com/JoshuaLM114/workwood/projectdef"
	"github.com/JoshuaLM114/workwood/superfeature"
	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
)

// ---- styles -----------------------------------------------------------------

var (
	docStyle   = lipgloss.NewStyle().Margin(1, 2)
	titleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("63")).Background(lipgloss.Color("236")).Padding(0, 1)
	dimStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	helpStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	errStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
	okStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("78"))
	warnStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
)

func tableStyles() table.Styles {
	s := table.DefaultStyles()
	s.Header = s.Header.Bold(true).Foreground(lipgloss.Color("252")).BorderBottom(true)
	s.Selected = s.Selected.Bold(true).Foreground(lipgloss.Color("231")).Background(lipgloss.Color("63"))
	return s
}

// ---- messages ---------------------------------------------------------------

type screen int

const (
	screenMenu     screen = iota // root navigation menu
	screenFeatures               // the super-features page
	screenCreate
	screenEditor
	screenActions
	screenRepos // the "edit project" repo editor
	screenSettings
)

type openEditorMsg struct{ feature string }
type openCreateMsg struct{}
type openFeaturesMsg struct{}
type openReposMsg struct{}
type openSettingsMsg struct{}
type backMsg struct{}
type applyDoneMsg struct {
	res *superfeature.EditResult
	err error
}
type upDoneMsg struct {
	log []string
	err error
}

// ---- root model -------------------------------------------------------------

// Model is the root Bubble Tea model that switches between the picker, the
// create-feature form, and the editor.
type Model struct {
	cfg *config.Config
	pd  *projectdef.File

	screen   screen
	menu     menuModel
	features *featuresModel
	editor   *editorModel
	actions  *actionsModel
	repos    *reposModel

	createForm *huh.Form
	createVals createVals

	settingsForm  *huh.Form
	settingsVals  settingsVals
	settingsApp   *config.AppSettings
	settingsState *config.ProjectState
	settingsHome  string

	width, height int
	err           error
}

// Run starts the TUI for an already-resolved project at the root menu.
func Run(cfg *config.Config, pd *projectdef.File) error {
	m := &Model{cfg: cfg, pd: pd, screen: screenMenu}
	_, err := tea.NewProgram(m, tea.WithAltScreen()).Run()
	return err
}

func (m *Model) Init() tea.Cmd { return nil }

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		hw, hh := msg.Width-4, msg.Height-4
		if m.features != nil {
			m.features.list.SetSize(hw, hh)
		}
		if m.editor != nil {
			m.editor.setSize(hw, hh)
		}
		if m.actions != nil {
			m.actions.setSize(hw, hh)
		}
		if m.repos != nil {
			m.repos.setSize(hw, hh)
		}
		return m, nil

	case openFeaturesMsg:
		m.features = newFeaturesModel(m)
		m.features.list.SetSize(m.width-4, m.height-4)
		m.screen = screenFeatures
		return m, nil

	case openReposMsg:
		m.repos = newReposModel(m)
		m.repos.setSize(m.width-4, m.height-4)
		m.screen = screenRepos
		return m, nil

	case openCreateMsg:
		// Block creation until the project's base repos are real clones.
		if err := superfeature.CheckReposReady(m.cfg); err != nil {
			if m.features != nil {
				m.features.status = errStyle.Render(err.Error())
			}
			return m, nil
		}
		m.createVals = createVals{}
		m.createForm = newCreateForm(m.cfg, &m.createVals).WithWidth(min(72, m.width-4))
		m.screen = screenCreate
		return m, m.createForm.Init()

	case openSettingsMsg:
		app, home, err := config.LoadApp()
		if err != nil {
			m.err = err
			return m, nil
		}
		st, err := config.LoadState(m.cfg.StateFile)
		if err != nil {
			m.err = err
			return m, nil
		}
		m.settingsApp, m.settingsState, m.settingsHome = app, st, home
		m.settingsVals = settingsVals{
			lang:        i18n.Lang(),
			updateCheck: app.UpdateCheckEnabled(),
			name:        m.cfg.ProjectName,
		}
		m.settingsForm = newSettingsForm(i18n.Supported, &m.settingsVals).WithWidth(min(72, m.width-4))
		m.screen = screenSettings
		return m, m.settingsForm.Init()

	case openEditorMsg:
		ed, err := newEditorModel(m, msg.feature)
		if err != nil {
			m.err = err
			return m, nil
		}
		m.editor = ed
		m.editor.setSize(m.width-4, m.height-4)
		m.screen = screenEditor
		return m, nil

	case openActionsMsg:
		am, err := newActionsModel(m, msg.feature)
		if err != nil {
			m.err = err
			return m, nil
		}
		m.actions = am
		m.actions.setSize(m.width-4, m.height-4)
		m.screen = screenActions
		return m, nil

	case backMsg:
		// Back walks the screen hierarchy: actions → editor → features → menu, and
		// repos → menu.
		switch m.screen {
		case screenActions:
			m.actions = nil
			m.screen = screenEditor
		case screenEditor:
			m.editor = nil
			m.features = newFeaturesModel(m)
			m.features.list.SetSize(m.width-4, m.height-4)
			m.screen = screenFeatures
		case screenRepos:
			m.repos = nil
			m.screen = screenMenu
		default: // screenFeatures and anything else → the root menu
			m.screen = screenMenu
		}
		return m, nil
	}

	switch m.screen {
	case screenCreate:
		return m.updateCreate(msg)
	case screenSettings:
		return m.updateSettings(msg)
	case screenEditor:
		var cmd tea.Cmd
		m.editor, cmd = m.editor.Update(msg)
		return m, cmd
	case screenActions:
		var cmd tea.Cmd
		m.actions, cmd = m.actions.Update(msg)
		return m, cmd
	case screenRepos:
		var cmd tea.Cmd
		m.repos, cmd = m.repos.Update(msg)
		return m, cmd
	case screenFeatures:
		// esc → back to the menu; q / ctrl+c quit — but only when no filter is
		// active, so those keys can still type into / cancel a filter.
		if k, ok := msg.(tea.KeyMsg); ok && m.features.list.FilterState() == 0 {
			switch k.String() {
			case "esc":
				return m, func() tea.Msg { return backMsg{} }
			case "q", "ctrl+c":
				return m, tea.Quit
			}
		}
		cmd := m.features.update(m, msg)
		return m, cmd
	default: // screenMenu (root): q / esc / ctrl+c exit
		if k, ok := msg.(tea.KeyMsg); ok {
			switch k.String() {
			case "q", "esc", "ctrl+c":
				return m, tea.Quit
			}
		}
		var cmd tea.Cmd
		m.menu, cmd = m.menu.update(m, msg)
		return m, cmd
	}
}

// updateCreate drives the new-feature huh form, then creates the manifest and
// hands off to the editor for the freshly made feature.
func (m *Model) updateCreate(msg tea.Msg) (tea.Model, tea.Cmd) {
	if k, ok := msg.(tea.KeyMsg); ok {
		switch k.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "esc": // back out to the features page without creating
			m.createForm = nil
			m.screen = screenFeatures
			return m, nil
		}
	}
	fm, cmd := m.createForm.Update(msg)
	if f, ok := fm.(*huh.Form); ok {
		m.createForm = f
	}
	switch m.createForm.State {
	case huh.StateCompleted:
		name := m.createVals.name
		if err := superfeature.Create(m.cfg, name, m.createVals.desc); err != nil {
			m.err = err
			m.screen = screenFeatures
			return m, nil
		}
		m.createForm = nil
		// Open the editor synchronously: if we just flipped to a screen whose model
		// isn't built yet (or left screenCreate with a nil form), the next render
		// would dereference nil. Build the editor now and switch in one step.
		ed, err := newEditorModel(m, name)
		if err != nil {
			m.err = err
			m.screen = screenFeatures
			return m, nil
		}
		m.editor = ed
		m.editor.setSize(m.width-4, m.height-4)
		m.screen = screenEditor
		return m, nil
	case huh.StateAborted:
		m.createForm = nil
		m.screen = screenFeatures
		return m, nil
	}
	return m, cmd
}

// updateSettings drives the dotfile-settings form, then writes the registry and
// applies the chosen language immediately. esc cancels without saving.
func (m *Model) updateSettings(msg tea.Msg) (tea.Model, tea.Cmd) {
	if k, ok := msg.(tea.KeyMsg); ok {
		switch k.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "esc": // cancel — back to the menu without saving
			m.settingsForm = nil
			m.screen = screenMenu
			return m, nil
		}
	}
	fm, cmd := m.settingsForm.Update(msg)
	if f, ok := fm.(*huh.Form); ok {
		m.settingsForm = f
	}
	switch m.settingsForm.State {
	case huh.StateCompleted:
		// Global app settings (language, update-check).
		app := m.settingsApp
		app.Language = m.settingsVals.lang
		uc := m.settingsVals.updateCheck
		app.UpdateCheck = &uc
		if err := config.SaveApp(m.settingsHome, app); err != nil {
			m.err = err
			return m, nil
		}
		// Project-local state (the active_name; checkout paths aren't editable).
		st := m.settingsState
		st.Project = m.cfg.ProjectID
		st.Name = m.settingsVals.name
		if err := config.SaveState(m.cfg.StateFile, st); err != nil {
			m.err = err
			return m, nil
		}
		// Apply immediately: switch language + update the display name.
		i18n.Init(m.settingsVals.lang)
		m.cfg.ProjectName = m.settingsVals.name
		m.settingsForm = nil
		m.screen = screenMenu
		return m, nil
	case huh.StateAborted:
		m.settingsForm = nil
		m.screen = screenMenu
		return m, nil
	}
	return m, cmd
}

func (m *Model) View() string {
	if m.err != nil {
		return docStyle.Render(errStyle.Render(i18n.T("tui.error", m.err.Error())) + "\n\n" + helpStyle.Render(i18n.T("tui.press_q")))
	}
	switch m.screen {
	case screenCreate:
		return docStyle.Render(titleStyle.Render(i18n.T("tui.new_feature")) + "\n\n" + m.createForm.View() + "\n" + helpStyle.Render(i18n.T("tui.form_help")))
	case screenSettings:
		return docStyle.Render(titleStyle.Render(i18n.T("tui.settings.title")) + "\n\n" + m.settingsForm.View() + "\n" + helpStyle.Render(i18n.T("tui.form_help")))
	case screenFeatures:
		return docStyle.Render(m.features.View())
	case screenEditor:
		return m.editor.View()
	case screenActions:
		return m.actions.View()
	case screenRepos:
		return m.repos.View()
	default:
		return m.menu.View(m)
	}
}

// pathExists reports whether a filesystem path exists.
func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
