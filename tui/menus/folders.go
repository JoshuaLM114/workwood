package menus

import (
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"

	"github.com/JoshuaLM114/workwood/i18n"
	"github.com/JoshuaLM114/workwood/superfeature"
	"github.com/JoshuaLM114/workwood/tui/components"
)

// FolderNamesModel reviews every outdated folder before opening a feature.
// Decisions are applied together only after the final page is submitted.
type FolderNamesModel struct {
	ctx       Ctx
	slug      string
	next      tea.Msg
	decisions []superfeature.FolderDecision
	form      *huh.Form
	spinner   spinner.Model
	busy      bool
	done      bool
	log       []string
	err       error
}

type folderNamesDoneMsg struct {
	log []string
	err error
}

// NewFolderNames returns nil when all recorded folders already follow the rule.
func NewFolderNames(ctx Ctx, slug string, next tea.Msg) (*FolderNamesModel, error) {
	changes, err := superfeature.CheckFolderNames(ctx.Cfg, slug)
	if err != nil || len(changes) == 0 {
		return nil, err
	}
	m := &FolderNamesModel{ctx: ctx, slug: slug, next: next, spinner: spinner.New(spinner.WithSpinner(spinner.Dot))}
	for _, change := range changes {
		m.decisions = append(m.decisions, superfeature.FolderDecision{FolderChange: change, Rename: true})
	}
	var groups []*huh.Group
	for i := range m.decisions {
		d := &m.decisions[i]
		description := i18n.T("folders.paths", d.Worktree.Path, d.Path)
		if d.Missing {
			description += "\n" + i18n.T("folders.missing")
		}
		groups = append(groups, huh.NewGroup(
			huh.NewNote().Title(d.Worktree.Repo+" — "+d.Worktree.Branch).Description(description),
			huh.NewSelect[bool]().Title(i18n.T("folders.question")).Options(
				huh.NewOption(i18n.T("folders.rename"), true),
				huh.NewOption(i18n.T("folders.drop"), false),
			).Value(&d.Rename),
		))
	}
	m.form = form(groups...)
	m.SetSize(ctx.Width, ctx.Height)
	return m, nil
}

func (m *FolderNamesModel) SetSize(w, h int) {
	m.form.WithWidth(max(30, min(80, w-4)))
}

func (m *FolderNamesModel) Init() tea.Cmd { return m.form.Init() }

func (m *FolderNamesModel) Update(msg tea.Msg) (*FolderNamesModel, tea.Cmd) {
	switch msg := msg.(type) {
	case folderNamesDoneMsg:
		m.busy, m.done, m.log, m.err = false, true, msg.log, msg.err
		return m, nil
	case spinner.TickMsg:
		if m.busy {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}
		return m, nil
	}
	if m.busy {
		return m, nil
	}
	if k, ok := msg.(tea.KeyMsg); ok {
		switch k.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "esc":
			return m, func() tea.Msg { return BackMsg{} }
		case "enter":
			if m.done {
				return m, func() tea.Msg { return m.next }
			}
		}
	}
	if m.done {
		return m, nil
	}
	fm, cmd := m.form.Update(msg)
	if f, ok := fm.(*huh.Form); ok {
		m.form = f
	}
	switch m.form.State {
	case huh.StateCompleted:
		m.busy = true
		cfg, slug := m.ctx.Cfg, m.slug
		decisions := append([]superfeature.FolderDecision(nil), m.decisions...)
		return m, tea.Batch(m.spinner.Tick, func() tea.Msg {
			log, err := superfeature.ApplyFolderNames(cfg, slug, decisions)
			return folderNamesDoneMsg{log: log, err: err}
		})
	case huh.StateAborted:
		return m, func() tea.Msg { return BackMsg{} }
	}
	return m, cmd
}

func (m *FolderNamesModel) View() string {
	title := components.TitleStyle.Render(i18n.T("folders.title", m.slug))
	if m.busy {
		return components.DocStyle.Render(title + "\n\n" + m.spinner.View() + " " + i18n.T("folders.applying"))
	}
	if m.done {
		body := strings.Join(m.log, "\n")
		help := i18n.T("folders.continue")
		if m.err != nil {
			body = components.ErrStyle.Render(m.err.Error())
			help = i18n.T("folders.retry")
		}
		return components.DocStyle.Render(title + "\n\n" + body + "\n\n" + components.HelpStyle.Render(help))
	}
	return components.DocStyle.Render(title + "\n\n" + m.form.View() + "\n" + components.HelpStyle.Render(i18n.T("folders.help")))
}
