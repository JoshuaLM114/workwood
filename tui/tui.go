// Package tui is the Bubble Tea front-end. It opens a root menu (Super-features ·
// Edit project · Settings); the super-features page drops into a malleable editor
// where worktrees can be staged for addition/removal and applied as one delta, and
// "Edit project" manages the base repos in workwood.yml. Forms use huh.
//
// The TUI operates on ONE already-resolved project (chosen via -p or cwd). This
// root package holds the shared state, the active screen, and the routing between
// screens; the screen models themselves live in the menus sub-package, and shared
// styles in the components sub-package.
package tui

import (
	"os"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/JoshuaLM114/workwood/config"
	"github.com/JoshuaLM114/workwood/i18n"
	"github.com/JoshuaLM114/workwood/models"
	"github.com/JoshuaLM114/workwood/repos"
	"github.com/JoshuaLM114/workwood/superfeature"
	"github.com/JoshuaLM114/workwood/tui/components"
	"github.com/JoshuaLM114/workwood/tui/menus"
)

// ---- screens ----------------------------------------------------------------

type screen int

const (
	screenMenu     screen = iota // root navigation menu
	screenFeatures               // the super-features page
	screenCreate
	screenEditor
	screenActions
	screenRepos // the "edit project" repo editor
	screenSettings
	screenDelete    // the delete-super-feature walkthrough
	screenReconcile // the manifest↔disk reconcile walkthrough
	screenFolderNames
)

// ---- root model -------------------------------------------------------------

// Model is the root Bubble Tea model that switches between the menu and the screen
// models (held as menus.* sub-models) and routes navigation messages between them.
type Model struct {
	cfg *models.Config
	pd  *models.ProjectDef

	screen    screen
	menu      menus.MenuModel
	features  *menus.FeaturesModel
	editor    *menus.EditorModel
	actions   *menus.ActionsModel
	repos     *menus.ReposModel
	delete    *menus.DeleteModel
	reconcile *menus.ReconcileModel
	create    *menus.CreateModel
	settings  *menus.SettingsModel
	folders   *menus.FolderNamesModel

	width, height int
	err           error
	syncWarning   string   // set on boot when ref repos are behind origin
	linkNotice    string   // set on boot when feature back-links need repair (or after a fix)
	brokenLinks   []string // feature slugs whose .workwood/link.yml is missing/stale
	actionsToMenu bool     // launched into Actions from a feature folder → esc goes to the menu
	actionsSlug   string
}

// ctx snapshots the shared, read-only project state for building a screen model.
func (m *Model) ctx() menus.Ctx {
	return menus.Ctx{Cfg: m.cfg, Pd: m.pd, Width: m.width, Height: m.height}
}

// bootSyncMsg carries the on-boot health check: ref repos behind origin, and
// feature folders whose back-link needs repair.
type bootSyncMsg struct {
	outOfSync   []string
	brokenLinks []string
}

// bootFetchCmd runs the on-boot health check in the background: a read-only fetch
// of the ref repos (never pulls), plus a (local, instant) scan for feature folders
// whose back-link is missing or stale.
func (m *Model) bootFetchCmd() tea.Cmd {
	cfg := m.cfg
	// Snapshot the repo list. The live m.pd.Repos can be reassigned by the repos
	// editor (add/remove/edit-branch) on the Update goroutine while this background
	// fetch ranges over it — copy the slice so the goroutine shares nothing the
	// event loop mutates (otherwise a -race-detectable data race).
	pd := *m.pd
	pd.Repos = append([]models.Repo(nil), m.pd.Repos...)
	return func() tea.Msg {
		repos.FetchAll(cfg, &pd)
		return bootSyncMsg{outOfSync: repos.OutOfSync(cfg, &pd), brokenLinks: brokenFeatureLinks(cfg)}
	}
}

// brokenFeatureLinks returns the slugs of built features (those with a folder on
// disk) whose .workwood/link.yml is missing, an old version, or inconsistent.
func brokenFeatureLinks(cfg *models.Config) []string {
	st, err := config.LoadState(cfg.StateFile)
	if err != nil {
		return nil
	}
	slugs := make([]string, 0, len(st.Features))
	for _, fs := range st.Features {
		slugs = append(slugs, fs.Slug)
	}
	sort.Strings(slugs)
	var broken []string
	for _, s := range slugs {
		if _, err := os.Stat(cfg.FeatureDir(s)); err != nil {
			continue // not built yet → nothing to validate
		}
		if !config.FeatureLinkValid(cfg, s) {
			broken = append(broken, s)
		}
	}
	return broken
}

// Run starts the TUI for an already-resolved project. Normally it opens at the
// root menu; when launched from inside a feature folder (cfg.ActiveFeature set) it
// jumps straight to that feature's Actions panel, with esc wired back to the menu
// so the parent yamls (repos, manifests, settings) stay reachable.
func Run(cfg *models.Config, pd *models.ProjectDef) error {
	m := &Model{cfg: cfg, pd: pd, screen: screenMenu}
	m.menu = menus.NewMenu(m.ctx(), menus.Notices{})
	if cfg.ActiveFeature != "" {
		m.actionsToMenu = true
	}
	_, err := tea.NewProgram(m, tea.WithAltScreen()).Run()
	return err
}

func (m *Model) Init() tea.Cmd {
	if m.cfg.ActiveFeature != "" {
		return tea.Batch(m.bootFetchCmd(), func() tea.Msg {
			return menus.OpenActionsMsg{Feature: m.cfg.ActiveFeature}
		})
	}
	return m.bootFetchCmd()
}

// checkFolderNames gates both editor and Actions entry, including a cwd launch.
func (m *Model) checkFolderNames(slug string, next tea.Msg) (tea.Cmd, bool) {
	folders, err := menus.NewFolderNames(m.ctx(), slug, next)
	if err != nil {
		m.err = err
		return nil, true
	}
	if folders == nil {
		return nil, false
	}
	m.folders = folders
	m.editor = nil
	m.actions = nil
	m.screen = screenFolderNames
	return folders.Init(), true
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		hw, hh := msg.Width-4, msg.Height-4
		if m.features != nil {
			m.features.SetSize(hw, hh)
		}
		if m.editor != nil {
			m.editor.SetSize(hw, hh)
		}
		if m.actions != nil {
			m.actions.SetSize(hw, hh)
		}
		if m.repos != nil {
			m.repos.SetSize(hw, hh)
		}
		if m.folders != nil {
			m.folders.SetSize(hw, hh)
		}
		return m, nil

	case bootSyncMsg:
		if len(msg.outOfSync) > 0 {
			m.syncWarning = i18n.T("tui.menu.out_of_sync", strings.Join(msg.outOfSync, ", "))
		}
		m.brokenLinks = msg.brokenLinks
		if len(msg.brokenLinks) > 0 {
			m.linkNotice = components.WarnStyle.Render(i18n.T("tui.menu.links_broken", strings.Join(msg.brokenLinks, ", ")))
		}
		m.menu.SetNotices(menus.Notices{SyncWarning: m.syncWarning, LinkNotice: m.linkNotice, BrokenLinks: m.brokenLinks})
		return m, nil

	case menus.ErrMsg:
		m.err = msg.Err
		return m, nil

	case menus.OpenFeaturesMsg:
		m.features = menus.NewFeatures(m.ctx())
		m.features.SetSize(m.width-4, m.height-4)
		m.screen = screenFeatures
		return m, nil

	case menus.OpenReposMsg:
		m.repos = menus.NewRepos(m.ctx())
		m.repos.SetSize(m.width-4, m.height-4)
		m.screen = screenRepos
		return m, nil

	case menus.OpenCreateMsg:
		// Block creation until the project's base repos are real clones.
		if err := superfeature.CheckReposReady(m.cfg); err != nil {
			if m.features != nil {
				m.features.SetStatus(components.ErrStyle.Render(err.Error()))
			}
			return m, nil
		}
		m.create = menus.NewCreate(m.ctx())
		m.screen = screenCreate
		return m, m.create.Init()

	case menus.OpenSettingsMsg:
		sm, err := menus.NewSettings(m.ctx())
		if err != nil {
			m.err = err
			return m, nil
		}
		m.settings = sm
		m.screen = screenSettings
		return m, m.settings.Init()

	case menus.OpenEditorMsg:
		if cmd, handled := m.checkFolderNames(msg.Feature, msg); handled {
			return m, cmd
		}
		ed, err := menus.NewEditor(m.ctx(), msg.Feature)
		if err != nil {
			m.err = err
			return m, nil
		}
		m.editor = ed
		m.editor.SetSize(m.width-4, m.height-4)
		m.screen = screenEditor
		return m, nil

	case menus.OpenActionsMsg:
		if cmd, handled := m.checkFolderNames(msg.Feature, msg); handled {
			return m, cmd
		}
		am, err := menus.NewActions(m.ctx(), msg.Feature)
		if err != nil {
			m.err = err
			return m, nil
		}
		m.actions = am
		m.actionsSlug = msg.Feature
		m.actions.SetSize(m.width-4, m.height-4)
		m.screen = screenActions
		return m, nil

	case menus.OpenDeleteMsg:
		dm, err := menus.NewDelete(m.ctx(), msg.Feature, msg.Name)
		if err != nil {
			m.err = err
			return m, nil
		}
		m.delete = dm
		m.screen = screenDelete
		return m, m.delete.Init()

	case menus.OpenReconcileMsg:
		rm, err := menus.NewReconcile(m.ctx(), msg.Feature)
		if err != nil {
			m.err = err
			return m, nil
		}
		m.reconcile = rm
		m.screen = screenReconcile
		return m, m.reconcile.Init()

	case menus.BackMsg:
		// Back walks the screen hierarchy: actions → editor → features → menu, and
		// repos → menu.
		switch m.screen {
		case screenActions:
			if m.actionsToMenu {
				m.actionsToMenu = false
				m.screen = screenMenu // launched from a feature folder → full menu
			} else {
				if m.editor == nil {
					slug := m.actionsSlug
					return m, func() tea.Msg { return menus.OpenEditorMsg{Feature: slug} }
				}
				m.screen = screenEditor
			}
			m.actions = nil
		case screenFolderNames:
			m.folders = nil
			if m.actionsToMenu {
				m.actionsToMenu = false
				m.screen = screenMenu
			} else {
				m.features = menus.NewFeatures(m.ctx())
				m.features.SetSize(m.width-4, m.height-4)
				m.screen = screenFeatures
			}
		case screenEditor:
			m.editor = nil
			m.features = menus.NewFeatures(m.ctx())
			m.features.SetSize(m.width-4, m.height-4)
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
	case screenFolderNames:
		var cmd tea.Cmd
		m.folders, cmd = m.folders.Update(msg)
		return m, cmd
	case screenCreate:
		var cmd tea.Cmd
		m.create, cmd = m.create.Update(msg)
		return m, cmd
	case screenSettings:
		var cmd tea.Cmd
		m.settings, cmd = m.settings.Update(msg)
		return m, cmd
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
	case screenDelete:
		var cmd tea.Cmd
		m.delete, cmd = m.delete.Update(msg)
		return m, cmd
	case screenReconcile:
		var cmd tea.Cmd
		m.reconcile, cmd = m.reconcile.Update(msg)
		return m, cmd
	case screenFeatures:
		// esc → back to the menu; q / ctrl+c quit — but only when no filter is
		// active, so those keys can still type into / cancel a filter.
		if k, ok := msg.(tea.KeyMsg); ok && m.features.FilterState() == 0 {
			switch k.String() {
			case "esc":
				return m, func() tea.Msg { return menus.BackMsg{} }
			case "q", "ctrl+c":
				return m, tea.Quit
			}
		}
		cmd := m.features.Update(msg)
		return m, cmd
	default: // screenMenu (root): q / esc / ctrl+c exit
		if k, ok := msg.(tea.KeyMsg); ok {
			switch k.String() {
			case "q", "esc", "ctrl+c":
				return m, tea.Quit
			}
		}
		var cmd tea.Cmd
		m.menu, cmd = m.menu.Update(msg)
		return m, cmd
	}
}

func (m *Model) View() string {
	if m.err != nil {
		return components.DocStyle.Render(components.ErrStyle.Render(i18n.T("tui.error", m.err.Error())) + "\n\n" + components.HelpStyle.Render(i18n.T("tui.press_q")))
	}
	switch m.screen {
	case screenFolderNames:
		return m.folders.View()
	case screenCreate:
		return m.create.View()
	case screenSettings:
		return m.settings.View()
	case screenFeatures:
		return components.DocStyle.Render(m.features.View())
	case screenEditor:
		return m.editor.View()
	case screenActions:
		return m.actions.View()
	case screenRepos:
		return m.repos.View()
	case screenDelete:
		return m.delete.View()
	case screenReconcile:
		return m.reconcile.View()
	default:
		return m.menu.View()
	}
}
