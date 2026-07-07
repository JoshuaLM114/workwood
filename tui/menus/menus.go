// Package menus holds the workwood TUI's screen models: the root navigation menu,
// the super-features page, the feature editor, the Actions panel, the repo editor,
// and the create/settings/delete/reconcile walkthroughs. Each screen is a
// self-contained Bubble Tea (sub-)model built from a Ctx (the shared, read-only
// project state) and driven by the root tui package, which owns routing.
//
// menus imports the components package (shared styles) and the domain packages it
// operates on, but NOT the root tui package — screens communicate navigation
// intent back up via the exported Open*/Back messages rather than calling root.
package menus

import (
	"github.com/charmbracelet/huh"

	"github.com/JoshuaLM114/workwood/models"
	"github.com/JoshuaLM114/workwood/superfeature"
)

// Ctx is the shared, read-only project state every screen model needs. The root
// tui.Model builds a fresh Ctx (snapshotting its current width/height) each time it
// constructs a screen.
type Ctx struct {
	Cfg    *models.Config
	Pd     *models.ProjectDef
	Width  int
	Height int
}

// Notices carries the on-boot health check results into the root menu screen: ref
// repos behind origin, and feature back-links needing repair.
type Notices struct {
	SyncWarning string   // ref repos behind origin (already plain text)
	LinkNotice  string   // already-styled (warn or ok) feature back-link notice
	BrokenLinks []string // feature slugs whose .workwood/link.yml is missing/stale
}

// ---- navigation messages ----------------------------------------------------
//
// Screens emit these to ask the root to switch screens; the root builds the next
// model and updates its routing. The per-screen async messages stay unexported in
// their own files (only that screen's model handles them).

type OpenEditorMsg struct{ Feature string }
type OpenActionsMsg struct{ Feature string }
type OpenDeleteMsg struct{ Feature, Name string }
type OpenReconcileMsg struct{ Feature string }
type OpenCreateMsg struct{}
type OpenFeaturesMsg struct{}
type OpenReposMsg struct{}
type OpenSettingsMsg struct{}
type BackMsg struct{}

// ErrMsg asks the root to surface a fatal error on its error screen (e.g. a
// create/settings flow that failed to persist). The root sets m.err from it.
type ErrMsg struct{ Err error }

// ---- shared form helpers ----------------------------------------------------

// form builds a huh form with the library's English key-hint footer disabled —
// the TUI renders its own localized footer (tui.form_help) instead.
func form(groups ...*huh.Group) *huh.Form {
	return huh.NewForm(groups...).WithShowHelp(false)
}

// applyDoneMsg / upDoneMsg are the editor's async results (kept here because the
// editor and the create flow both reference EditResult-shaped completions). They
// are handled only by the editor model.
type applyDoneMsg struct {
	res *superfeature.EditResult
	err error
}
type upDoneMsg struct {
	log []string
	err error
}
