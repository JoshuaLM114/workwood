package tui

import (
	"os"
	"strings"

	"github.com/JoshuaLM114/workwood/config"
	"github.com/JoshuaLM114/workwood/i18n"
	"github.com/charmbracelet/huh"
)

// form builds a huh form with the library's English key-hint footer disabled —
// the TUI renders its own localized footer (tui.form_help) instead.
func form(groups ...*huh.Group) *huh.Form {
	return huh.NewForm(groups...).WithShowHelp(false)
}

// required rejects blank input.
func required(s string) error {
	if strings.TrimSpace(s) == "" {
		return i18n.Err("tui.form.required")
	}
	return nil
}

// createVals holds the new-feature form bindings.
type createVals struct {
	name string
	desc string
}

// newCreateForm builds the "create a new super-feature" form. The name must be
// non-empty, free of whitespace/slashes (it becomes a branch prefix, a dir, and
// a manifest filename), and must not collide with an existing manifest.
func newCreateForm(cfg *config.Config, v *createVals) *huh.Form {
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
			huh.NewText().
				Key("desc").
				Title(i18n.T("tui.form.description")).
				Description(i18n.T("tui.form.description_desc")).
				Value(&v.desc),
		),
	)
}

// addVals holds the add-worktree form bindings.
type addVals struct {
	repo       string
	sub        string
	from       string
	omitPrefix bool
}

// newAddForm builds the "add a worktree" form.
func newAddForm(repos []string, v *addVals) *huh.Form {
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
			huh.NewInput().
				Key("sub").
				Title(i18n.T("tui.form.wt_branch")).
				Description(i18n.T("tui.form.wt_branch_desc")).
				Value(&v.sub).
				Validate(required),
			huh.NewInput().
				Key("from").
				Title(i18n.T("tui.form.source")).
				Description(i18n.T("tui.form.source_desc")).
				Value(&v.from),
			huh.NewConfirm().
				Key("omitPrefix").
				Title(i18n.T("tui.form.strip")).
				Description(i18n.T("tui.form.strip_desc")).
				Affirmative(i18n.T("tui.form.strip_yes")).
				Negative(i18n.T("tui.form.strip_no")).
				Value(&v.omitPrefix),
		),
	)
}

// targetOption is one choice in the setup picker: a label and the encoded value
// ("ignore", "main", "worktree", "wt:<manifest-path>", or "custom").
type targetOption struct {
	label string
	value string
}

// setupVals binds a setup picker: a chosen built-in/worktree, or a custom string.
type setupVals struct {
	choice string
	custom string
}

// setupSelect builds the shared "choose a setup" select.
func setupSelect(wtOpts []targetOption, v *setupVals) []huh.Field {
	options := []huh.Option[string]{
		huh.NewOption(i18n.T("tui.form.setup_worktree_primary"), "worktree"),
	}
	for _, o := range wtOpts {
		options = append(options, huh.NewOption(o.label, o.value))
	}
	options = append(options,
		huh.NewOption(i18n.T("tui.form.setup_main"), "main"),
		huh.NewOption(i18n.T("tui.form.setup_ignore"), "ignore"),
		huh.NewOption(i18n.T("tui.form.setup_custom"), "custom"),
	)
	if v.choice == "" {
		v.choice = "worktree"
	}
	return []huh.Field{
		huh.NewSelect[string]().
			Key("setup").
			Title(i18n.T("tui.form.setup")).
			Description(i18n.T("tui.form.setup_desc")).
			Options(options...).
			Value(&v.choice),
		huh.NewInput().
			Key("custom").
			Title(i18n.T("tui.form.custom")).
			Description(i18n.T("tui.form.custom_desc")).
			Value(&v.custom),
	}
}

// newAddTargetForm builds the per-repo "add a setup" form.
func newAddTargetForm(repo string, wtOpts []targetOption, v *setupVals) *huh.Form {
	return form(huh.NewGroup(setupSelect(wtOpts, v)...).Title(i18n.T("tui.form.add_setup_for", repo)))
}

// bulkVals binds the bulk form: a set of repos plus a setup.
type bulkVals struct {
	repos []string
	setup setupVals
}

// newBulkTargetForm builds the "add a setup to many repos" form.
func newBulkTargetForm(repos []string, v *bulkVals) *huh.Form {
	opts := make([]huh.Option[string], 0, len(repos))
	for _, r := range repos {
		opts = append(opts, huh.NewOption(r, r))
	}
	if v.repos == nil {
		v.repos = append([]string{}, repos...) // default: all selected
	}
	fields := []huh.Field{
		huh.NewMultiSelect[string]().
			Key("repos").
			Title(i18n.T("tui.form.repos")).
			Description(i18n.T("tui.form.repos_desc")).
			Options(opts...).
			Value(&v.repos),
	}
	fields = append(fields, setupSelect(nil, &v.setup)...)
	return form(huh.NewGroup(fields...).Title(i18n.T("tui.form.bulk_title")))
}

// settingsVals binds the settings form to ~/.workwood/config.yaml fields.
type settingsVals struct {
	lang        string
	defProject  string
	mainDir     string
	featuresDir string
}

// newSettingsForm builds the dotfile-settings editor: UI language, default
// project, and the active project's on-disk paths. Submitting saves; esc cancels.
func newSettingsForm(langs, projects []string, project string, v *settingsVals) *huh.Form {
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
	projOpts := make([]huh.Option[string], 0, len(projects))
	for _, p := range projects {
		projOpts = append(projOpts, huh.NewOption(p, p))
	}
	return form(
		huh.NewGroup(
			huh.NewSelect[string]().
				Key("lang").
				Title(i18n.T("tui.settings.lang")).
				Description(i18n.T("tui.settings.lang_desc")).
				Options(langOpts...).
				Value(&v.lang),
			huh.NewSelect[string]().
				Key("def").
				Title(i18n.T("tui.settings.default_project")).
				Description(i18n.T("tui.settings.default_project_desc")).
				Options(projOpts...).
				Value(&v.defProject),
			huh.NewInput().
				Key("main").
				Title(i18n.T("tui.settings.main_dir", project)).
				Value(&v.mainDir).
				Validate(required),
			huh.NewInput().
				Key("features").
				Title(i18n.T("tui.settings.features_dir", project)).
				Value(&v.featuresDir).
				Validate(required),
		).Title(i18n.T("tui.settings.title")),
	)
}

// metaVals holds the edit-description form binding.
type metaVals struct {
	desc string
}

// newMetaForm edits a feature's description (the name is fixed once created).
func newMetaForm(v *metaVals) *huh.Form {
	return form(
		huh.NewGroup(
			huh.NewText().
				Key("desc").
				Title(i18n.T("tui.form.description")).
				Value(&v.desc),
		),
	)
}
