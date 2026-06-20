// Package tui is the Bubble Tea front-end. It opens a picker of the selected
// project's super-features and drops into a malleable editor where worktrees can
// be staged for addition/removal and applied as one delta — so editing an
// existing feature, not just creating one, is the primary flow. Forms use huh.
//
// The TUI operates on ONE already-selected project (chosen via -p, cwd, or the
// default); switching projects is a CLI concern (`workwood project use`).
package tui

import (
	"os"
	"path/filepath"

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
	screenList screen = iota
	screenCreate
	screenEditor
	screenSettings
)

type openEditorMsg struct{ feature string }
type openCreateMsg struct{}
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

	screen screen
	list   listModel
	editor *editorModel

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

// Run starts the TUI for an already-resolved project.
func Run(cfg *config.Config, pd *projectdef.File) error {
	m := &Model{cfg: cfg, pd: pd, screen: screenList}
	m.list = newListModel(m)
	_, err := tea.NewProgram(m, tea.WithAltScreen()).Run()
	return err
}

func (m *Model) Init() tea.Cmd { return nil }

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		hw, hh := msg.Width-4, msg.Height-4
		m.list.list.SetSize(hw, hh)
		if m.editor != nil {
			m.editor.setSize(hw, hh)
		}
		return m, nil

	case openCreateMsg:
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
			mainDir:     m.cfg.MainDir,
			featuresDir: m.cfg.FeaturesDir,
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

	case backMsg:
		m.editor = nil
		m.list = newListModel(m)
		m.list.list.SetSize(m.width-4, m.height-4)
		m.screen = screenList
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
	default:
		// The list is the root menu: q / esc / ctrl+c all exit (esc only when no
		// filter is active, so it can still clear/cancel a filter first).
		if k, ok := msg.(tea.KeyMsg); ok && (k.String() == "q" || k.String() == "esc" || k.String() == "ctrl+c") {
			if m.list.list.FilterState() == 0 { // not filtering
				return m, tea.Quit
			}
		}
		var cmd tea.Cmd
		m.list, cmd = m.list.update(m, msg)
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
		case "esc": // back out to the feature list without creating
			m.createForm = nil
			m.screen = screenList
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
			m.screen = screenList
			return m, nil
		}
		m.createForm = nil
		return m, func() tea.Msg { return openEditorMsg{feature: name} }
	case huh.StateAborted:
		m.createForm = nil
		m.screen = screenList
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
		case "esc": // cancel — back to the list without saving
			m.settingsForm = nil
			m.screen = screenList
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
		// Project-local state (active_name + checkout-path overrides).
		st := m.settingsState
		st.Project = m.cfg.ProjectID
		st.Name = m.settingsVals.name
		mainDir := m.settingsVals.mainDir
		if abs, err := filepath.Abs(mainDir); err == nil {
			mainDir = abs
		}
		featuresDir := m.settingsVals.featuresDir
		if abs, err := filepath.Abs(featuresDir); err == nil {
			featuresDir = abs
		}
		st.MainDir, st.FeaturesDir = mainDir, featuresDir
		if err := config.SaveState(m.cfg.StateFile, st); err != nil {
			m.err = err
			return m, nil
		}
		// Apply immediately: switch language + repoint the active config.
		i18n.Init(m.settingsVals.lang)
		m.cfg.MainDir, m.cfg.FeaturesDir = mainDir, featuresDir
		m.cfg.ProjectName = m.settingsVals.name
		m.settingsForm = nil
		m.list = newListModel(m)
		m.list.list.SetSize(m.width-4, m.height-4)
		m.screen = screenList
		return m, nil
	case huh.StateAborted:
		m.settingsForm = nil
		m.screen = screenList
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
	case screenEditor:
		return m.editor.View()
	default:
		return docStyle.Render(m.list.View())
	}
}

// pathExists reports whether a filesystem path exists.
func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
