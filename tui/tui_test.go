package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// TestWindowSizeOnMenuNoPanic guards the startup crash where a WindowSizeMsg
// arrived while sitting on the root menu — the features list isn't built yet, so
// sizing it must be skipped rather than dereferencing a zero list.Model.
func TestWindowSizeOnMenuNoPanic(t *testing.T) {
	m := &Model{screen: screenMenu}
	// Would panic before the nil-guard on m.features.
	m.Update(tea.WindowSizeMsg{Width: 207, Height: 51})
	if m.width != 207 || m.height != 51 {
		t.Fatalf("size not recorded: %dx%d", m.width, m.height)
	}
}

// TestMenuNavNoPanic exercises arrow-key navigation on the root menu.
func TestMenuNavNoPanic(t *testing.T) {
	m := &Model{screen: screenMenu}
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	for range menuOrder {
		m.Update(tea.KeyMsg{Type: tea.KeyDown})
	}
	m.Update(tea.KeyMsg{Type: tea.KeyUp})
}
