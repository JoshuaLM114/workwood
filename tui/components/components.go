// Package components holds the workwood TUI's shared, model-independent UI pieces:
// the lipgloss styles and table styling reused across the menu screens. It depends
// on nothing in tui, so both the root model and the menus package can import it.
package components

import (
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/lipgloss"
)

// Shared text styles.
var (
	DocStyle         = lipgloss.NewStyle().Margin(1, 2)
	TitleStyle       = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("63")).Background(lipgloss.Color("236")).Padding(0, 1)
	DimStyle         = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	HelpStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	ErrStyle         = lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
	OkStyle          = lipgloss.NewStyle().Foreground(lipgloss.Color("78"))
	WarnStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	SelectedRowStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("231")).Background(lipgloss.Color("63"))
)

// TableStyles is the shared bubbles/table styling: bold header, highlighted row.
func TableStyles() table.Styles {
	s := table.DefaultStyles()
	s.Header = s.Header.Bold(true).Foreground(lipgloss.Color("252")).BorderBottom(true)
	s.Selected = s.Selected.Bold(true).Foreground(lipgloss.Color("231")).Background(lipgloss.Color("63"))
	return s
}
