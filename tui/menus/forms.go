package menus

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/huh"

	"github.com/JoshuaLM114/workwood/i18n"
	"github.com/JoshuaLM114/workwood/manifest"
	"github.com/JoshuaLM114/workwood/models"
	"github.com/JoshuaLM114/workwood/repos"
	"github.com/JoshuaLM114/workwood/superfeature"
)

// required rejects blank input.
func required(s string) error {
	if strings.TrimSpace(s) == "" {
		return i18n.Err("tui.form.required")
	}
	return nil
}

// createVals holds the new-feature form bindings.
type createVals struct {
	name      string
	shorthand string
	desc      string
}

// newCreateForm builds the "create a new super-feature" form. The name must be
// non-empty, free of whitespace/slashes (it becomes a branch prefix, a dir, and
// a manifest filename), and must not collide with an existing manifest.
func newCreateForm(cfg *models.Config, v *createVals) *huh.Form {
	return form(
		huh.NewGroup(
			huh.NewInput().
				Key("name").
				Title(i18n.T("tui.form.feature_name")).
				Description(i18n.T("tui.form.feature_name_desc")).
				Value(&v.name).
				Validate(func(s string) error {
					s = strings.TrimSpace(s)
					if s == "" {
						return i18n.Err("tui.form.required")
					}
					if strings.ContainsAny(s, " \t/") {
						return i18n.Err("tui.form.no_spaces")
					}
					if _, err := os.Stat(cfg.ManifestPath(s)); err == nil {
						return i18n.Err("tui.form.name_exists")
					}
					return nil
				}),
			// The shorthand is the worktree branch prefix. Its placeholder shows the
			// auto-derived initials (live, as the name is typed); leaving it blank
			// accepts that default.
			huh.NewInput().
				Key("shorthand").
				Title(i18n.T("tui.form.shorthand")).
				Description(i18n.T("tui.form.shorthand_desc")).
				PlaceholderFunc(func() string { return manifest.DefaultShorthand(strings.TrimSpace(v.name)) }, &v.name).
				Value(&v.shorthand).
				Validate(func(s string) error {
					if strings.ContainsAny(strings.TrimSpace(s), " \t/") {
						return i18n.Err("tui.form.no_spaces")
					}
					return nil
				}),
			huh.NewText().
				Key("desc").
				Title(i18n.T("tui.form.description")).
				Description(i18n.T("tui.form.description_desc")).
				Value(&v.desc),
		),
	)
}

// addVals holds the add-worktree form bindings across its phases: phase 1 picks
// the repo + mode (new vs from-existing); phase 2 then collects either a new
// branch name (+ placement + source) or an existing branch to check out.
type addVals struct {
	repo         string
	fromExisting bool   // phase 1: false = new branch, true = check out an existing one
	sub          string // new-branch name (the part after <feature>/)
	from         string // new-branch source ref
	omitPrefix   bool   // new-branch placement: drop the <feature>/ prefix
	branch       string // chosen existing branch (fromExisting)
}

// newAddModeForm is phase 1 of adding a worktree: pick the repo, then choose
// between creating a new branch and checking out an existing one.
func newAddModeForm(repos []string, v *addVals) *huh.Form {
	opts := make([]huh.Option[string], 0, len(repos))
	for _, r := range repos {
		opts = append(opts, huh.NewOption(r, r))
	}
	if v.repo == "" && len(repos) > 0 {
		v.repo = repos[0]
	}
	return form(
		huh.NewGroup(
			huh.NewSelect[string]().
				Key("repo").
				Title(i18n.T("tui.form.repo")).
				Options(opts...).
				Value(&v.repo),
			huh.NewSelect[bool]().
				Key("mode").
				Title(i18n.T("tui.form.branch_mode")).
				Description(i18n.T("tui.form.branch_mode_desc")).
				Options(
					huh.NewOption(i18n.T("tui.form.mode_new"), false),
					huh.NewOption(i18n.T("tui.form.mode_existing"), true),
				).
				Value(&v.fromExisting),
		),
	)
}

// newAddForm is phase 2 for a NEW branch: its name, placement, and source ref.
// feature is the branch prefix, used to preview the exact branch each
// placement choice produces.
func newAddForm(feature string, v *addVals, validate func(string, bool) error) *huh.Form {
	return form(
		huh.NewGroup(
			huh.NewInput().
				Key("sub").
				Title(i18n.T("tui.form.wt_branch")).
				Description(i18n.T("tui.form.wt_branch_desc")).
				Value(&v.sub).
				Validate(required),
			// The branch is normally nested under the super-feature
			// (<feature>/<name>); a standalone branch drops that prefix. The options
			// re-render with the actual branch as the name is typed, so it's never
			// ambiguous what each choice produces.
			huh.NewSelect[bool]().
				Key("omitPrefix").
				Title(i18n.T("tui.form.branch_placement")).
				Description(i18n.T("tui.form.branch_placement_desc")).
				OptionsFunc(func() []huh.Option[bool] {
					name := strings.TrimSpace(v.sub)
					if name == "" {
						name = i18n.T("tui.form.name_ph")
					}
					return []huh.Option[bool]{
						huh.NewOption(i18n.T("tui.form.placement_nested", superfeature.ResolveBranch(feature, name)), false),
						huh.NewOption(i18n.T("tui.form.placement_standalone", name), true),
					}
				}, &v.sub).
				Value(&v.omitPrefix).
				Validate(func(omit bool) error { return validate(strings.TrimSpace(v.sub), omit) }),
			huh.NewInput().
				Key("from").
				Title(i18n.T("tui.form.source")).
				Description(i18n.T("tui.form.source_desc")).
				Value(&v.from),
		),
	)
}

// newAddExistingForm is phase 2 for an EXISTING branch: a dropdown of the repo's
// branches (each tagged local / remote / both). The worktree checks that branch
// out directly — a remote-only branch becomes a local branch tracking it.
func newAddExistingForm(branches []repos.BranchRef, v *addVals) *huh.Form {
	opts := make([]huh.Option[string], 0, len(branches))
	for _, b := range branches {
		opts = append(opts, huh.NewOption(branchOptionLabel(b), b.Name))
	}
	if v.branch == "" && len(branches) > 0 {
		v.branch = branches[0].Name
	}
	return form(
		huh.NewGroup(
			huh.NewSelect[string]().
				Key("branch").
				Title(i18n.T("tui.form.existing_branch")).
				Description(i18n.T("tui.form.existing_branch_desc")).
				Options(opts...).
				Value(&v.branch),
		),
	)
}

// ---- actions-screen forms -------------------------------------------------

// pathVals binds the add-arbitrary-path form. key is optional (a git path defaults
// to its branch; otherwise a key is required, validated by the caller).
type pathVals struct {
	path string
	key  string
}

// newAddPathForm builds the "add an arbitrary target path" form.
func newAddPathForm(v *pathVals) *huh.Form {
	return form(
		huh.NewGroup(
			huh.NewInput().
				Key("path").
				Title(i18n.T("tui.form.path")).
				Description(i18n.T("tui.form.path_desc")).
				Value(&v.path).
				Validate(required),
			huh.NewInput().
				Key("key").
				Title(i18n.T("tui.form.key")).
				Description(i18n.T("tui.form.key_desc")).
				Value(&v.key),
		),
	)
}

// keyVals binds the rename-key form.
type keyVals struct {
	key string
}

// newRenameKeyForm builds the "rename a target key" form.
func newRenameKeyForm(v *keyVals) *huh.Form {
	return form(
		huh.NewGroup(
			huh.NewInput().
				Key("key").
				Title(i18n.T("tui.form.key")).
				Description(i18n.T("tui.form.key_desc")).
				Value(&v.key).
				Validate(required),
		),
	)
}

// nameVals binds the save-preset form.
type nameVals struct {
	name string
}

// newSavePresetForm builds the "save target preset" form. v.name is pre-filled
// (with the last-loaded preset); existing preset names are offered as suggestions
// so you can pick one to overwrite or type a new one. Overwrite is confirmed
// separately (see the actions screen's save flow).
func newSavePresetForm(existing []string, v *nameVals) *huh.Form {
	return form(
		huh.NewGroup(
			huh.NewInput().
				Key("name").
				Title(i18n.T("tui.form.preset_name")).
				Suggestions(existing).
				Value(&v.name).
				Validate(func(s string) error {
					s = strings.TrimSpace(s)
					if s == "" {
						return i18n.Err("tui.form.required")
					}
					if strings.ContainsAny(s, " \t/") {
						return i18n.Err("tui.form.no_spaces")
					}
					return nil
				}),
		),
	)
}

// confirmVals binds a yes/no confirm form.
type confirmVals struct {
	ok bool
}

// newConfirmForm builds a single yes/no confirmation.
func newConfirmForm(title string, v *confirmVals) *huh.Form {
	return form(
		huh.NewGroup(
			huh.NewConfirm().
				Key("ok").
				Title(title).
				Value(&v.ok),
		),
	)
}

// repoVals binds the add-repo form (a base repo in workwood.yml).
type repoVals struct {
	name string
	url  string
}

// newAddRepoForm is step 1 of adding a repo: just the name (+ optional URL). The
// default branch is chosen in step 2 once the name lets us list the remote's
// branches — see newEditBranchForm, reused there.
func newAddRepoForm(v *repoVals) *huh.Form {
	return form(
		huh.NewGroup(
			huh.NewInput().
				Key("name").
				Title(i18n.T("tui.repos.form_name")).
				Description(i18n.T("tui.repos.form_name_desc")).
				Value(&v.name).
				Validate(func(s string) error {
					s = strings.TrimSpace(s)
					if s == "" {
						return i18n.Err("tui.form.required")
					}
					if strings.ContainsAny(s, " \t/") {
						return i18n.Err("tui.form.no_spaces")
					}
					return nil
				}),
			huh.NewInput().
				Key("url").
				Title(i18n.T("tui.repos.form_url")).
				Description(i18n.T("tui.repos.form_url_desc")).
				Value(&v.url).
				Validate(required),
		),
	)
}

// branchVals binds the edit-default-branch form.
type branchVals struct{ branch string }

// newEditBranchForm edits a repo's default branch (which also checks the base
// clone out to it). When the clone's branches are known it's a dropdown of them,
// each tagged local / remote / both; otherwise (repo not cloned yet) it falls back
// to free text.
func newEditBranchForm(branches []repos.BranchRef, v *branchVals) *huh.Form {
	if len(branches) == 0 {
		return form(
			huh.NewGroup(
				huh.NewInput().
					Key("branch").
					Title(i18n.T("tui.repos.form_branch")).
					Description(i18n.T("tui.repos.edit_branch_desc")).
					Value(&v.branch).
					Validate(required),
			),
		)
	}
	opts := make([]huh.Option[string], 0, len(branches))
	for _, b := range branches {
		opts = append(opts, huh.NewOption(branchOptionLabel(b), b.Name))
	}
	return form(
		huh.NewGroup(
			huh.NewSelect[string]().
				Key("branch").
				Title(i18n.T("tui.repos.form_branch")).
				Description(i18n.T("tui.repos.edit_branch_desc")).
				Options(opts...).
				Value(&v.branch),
		),
	)
}

// branchOptionLabel renders a branch with a small local/remote indicator.
func branchOptionLabel(b repos.BranchRef) string {
	var where []string
	if b.Local {
		where = append(where, i18n.T("tui.repos.br_local"))
	}
	if b.Remote {
		where = append(where, i18n.T("tui.repos.br_remote"))
	}
	return fmt.Sprintf("%-28s (%s)", b.Name, strings.Join(where, "·"))
}

// selectVals binds a single-choice select form.
type selectVals struct {
	choice string
}

// newSelectForm builds a one-field select (used for load-preset and run-action).
func newSelectForm(title string, opts []string, v *selectVals) *huh.Form {
	o := make([]huh.Option[string], 0, len(opts))
	for _, s := range opts {
		o = append(o, huh.NewOption(s, s))
	}
	return newSelectOptForm(title, o, v)
}

// newSelectOptForm is newSelectForm with pre-built options, so labels can differ
// from values (e.g. an unavailable-action marker on the label).
func newSelectOptForm(title string, opts []huh.Option[string], v *selectVals) *huh.Form {
	if v.choice == "" && len(opts) > 0 {
		v.choice = opts[0].Value
	}
	return form(
		huh.NewGroup(
			huh.NewSelect[string]().
				Key("sel").
				Title(title).
				Options(opts...).
				Value(&v.choice),
		),
	)
}

// settingsVals binds the settings form: global app settings (language,
// update-check) plus this project's local active_name. Checkout paths are NOT
// editable — they're fixed under WORKWOOD_DATA.
type settingsVals struct {
	lang        string
	updateCheck bool
	name        string // project active_name (local)
}

// newSettingsForm builds the settings editor. Submitting saves; esc cancels.
func newSettingsForm(langs []string, v *settingsVals) *huh.Form {
	endonym := map[string]string{
		"en": i18n.T("tui.settings.lang_en"),
		"ja": i18n.T("tui.settings.lang_ja"),
	}
	langOpts := make([]huh.Option[string], 0, len(langs))
	for _, c := range langs {
		label := endonym[c]
		if label == "" {
			label = c
		}
		langOpts = append(langOpts, huh.NewOption(label, c))
	}
	return form(
		huh.NewGroup(
			huh.NewSelect[string]().
				Key("lang").
				Title(i18n.T("tui.settings.lang")).
				Description(i18n.T("tui.settings.lang_desc")).
				Options(langOpts...).
				Value(&v.lang),
			huh.NewSelect[bool]().
				Key("update").
				Title(i18n.T("tui.settings.update_check")).
				Description(i18n.T("tui.settings.update_check_desc")).
				Options(
					huh.NewOption(i18n.T("tui.settings.update_on"), true),
					huh.NewOption(i18n.T("tui.settings.update_off"), false),
				).
				Value(&v.updateCheck),
			huh.NewInput().
				Key("name").
				Title(i18n.T("tui.settings.active_name")).
				Description(i18n.T("tui.settings.active_name_desc")).
				Value(&v.name),
		).Title(i18n.T("tui.settings.title")),
	)
}

// metaVals holds the edit-meta form binding (feature active_name + description).
type metaVals struct {
	name string
	desc string
}

// newMetaForm edits a feature's active_name + description. The slug (branch
// prefix / filename) is fixed once created and is not editable here.
func newMetaForm(v *metaVals) *huh.Form {
	return form(
		huh.NewGroup(
			huh.NewInput().
				Key("name").
				Title(i18n.T("tui.form.active_name")).
				Description(i18n.T("tui.form.active_name_desc")).
				Value(&v.name),
			huh.NewText().
				Key("desc").
				Title(i18n.T("tui.form.description")).
				Value(&v.desc),
		),
	)
}
