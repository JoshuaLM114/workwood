package menus

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"

	"github.com/JoshuaLM114/workwood/i18n"
	"github.com/JoshuaLM114/workwood/superfeature"
	"github.com/JoshuaLM114/workwood/tui/components"
)

// CreateModel drives the new-feature huh form, then creates the manifest and hands
// off to the editor (via OpenEditorMsg) for the freshly made feature. esc backs out
// to the features page without creating.
type CreateModel struct {
	ctx  Ctx
	form *huh.Form
	vals createVals
}

// NewCreate builds the create-feature form. The caller (root) only reaches this
// after confirming the project's base repos are real clones.
func NewCreate(ctx Ctx) *CreateModel {
	c := &CreateModel{ctx: ctx}
	c.form = newCreateForm(ctx.Cfg, &c.vals).WithWidth(min(72, ctx.Width-4))
	return c
}

// Init starts the form.
func (c *CreateModel) Init() tea.Cmd { return c.form.Init() }

func (c *CreateModel) Update(msg tea.Msg) (*CreateModel, tea.Cmd) {
	if k, ok := msg.(tea.KeyMsg); ok {
		switch k.String() {
		case "ctrl+c":
			return c, tea.Quit
		case "esc": // back out to the features page without creating
			return c, func() tea.Msg { return OpenFeaturesMsg{} }
		}
	}
	fm, cmd := c.form.Update(msg)
	if f, ok := fm.(*huh.Form); ok {
		c.form = f
	}
	switch c.form.State {
	case huh.StateCompleted:
		name := c.vals.name
		if err := superfeature.Create(c.ctx.Cfg, name, c.vals.shorthand, c.vals.desc); err != nil {
			return c, func() tea.Msg { return ErrMsg{Err: err} }
		}
		// Hand off to the editor for the freshly created feature.
		return c, func() tea.Msg { return OpenEditorMsg{Feature: name} }
	case huh.StateAborted:
		return c, func() tea.Msg { return OpenFeaturesMsg{} }
	}
	return c, cmd
}

func (c *CreateModel) View() string {
	return components.DocStyle.Render(components.TitleStyle.Render(i18n.T("tui.new_feature")) + "\n\n" + c.form.View() + "\n" + components.HelpStyle.Render(i18n.T("tui.form_help")))
}
