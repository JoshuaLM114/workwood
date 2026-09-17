// Command workwood orchestrates git worktrees across many repos, organised into
// "super-features" that can span repos and hold multiple branches of the same
// repo (which submodules can't).
//
// Projects are discovered by workwood.yml, feature back-links, or the local
// ~/.workwood/projects.yml registry. The super-repo holds committed definitions,
// actions and feature manifests; WORKWOOD_DATA holds clones, checkouts and local
// state. The MCP server exposes the same operations over local stdio.
//
// User-facing text is loaded from the message catalog (package i18n) so the CLI
// can run in English or Japanese; pick with `workwood lang ja` or $WORKWOOD_LANG.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"

	"gopkg.in/yaml.v3"

	"github.com/JoshuaLM114/workwood/action"
	"github.com/JoshuaLM114/workwood/config"
	"github.com/JoshuaLM114/workwood/i18n"
	"github.com/JoshuaLM114/workwood/manifest"
	"github.com/JoshuaLM114/workwood/mcpserver"
	"github.com/JoshuaLM114/workwood/models"
	"github.com/JoshuaLM114/workwood/projectdef"
	"github.com/JoshuaLM114/workwood/repos"
	"github.com/JoshuaLM114/workwood/superfeature"
	"github.com/JoshuaLM114/workwood/targetcfg"
	"github.com/JoshuaLM114/workwood/tui"
	"github.com/JoshuaLM114/workwood/update"
	"github.com/JoshuaLM114/workwood/version"
)

func main() {
	initLang() // resolve + load the message catalog before any output

	args := os.Args[1:]
	projectFlag, args := extractProjectFlag(args)

	cmd := ""
	if len(args) > 0 {
		cmd = args[0]
		args = args[1:]
	}

	if cmd == "mcp" {
		must(runMCP(projectFlag, args))
		return
	}
	if (cmd == "project" || cmd == "projects") && len(args) > 0 && args[0] == "check" {
		must(runProject(projectFlag, args))
		return
	}
	notifyIfOutdated()

	switch cmd {
	case "init":
		if pos, _ := splitFlags(args); len(pos) == 0 && projectFlag != "" {
			args = append([]string{projectFlag}, args...)
		}
		must(runInit(args))
	case "lang", "language":
		must(runLang(args))
	case "project", "projects":
		must(runProject(projectFlag, args))
	case "repos":
		must(runRepos(projectFlag, args))
	case "super-feature", "sf", "feature":
		must(runFeature(projectFlag, args))
	case "action":
		must(runAction(projectFlag, args))
	case "actions":
		must(runActions(projectFlag))
	case "targets":
		must(runTargets(projectFlag, args))
	case "version", "--version", "-v":
		fmt.Println(i18n.T("cli.version", version.Software, version.Schema))
	case "":
		must(runTUI(projectFlag))
	case "-h", "--help", "help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "%s\n\n", i18n.T("err.unknown_command", cmd))
		usage()
		os.Exit(2)
	}
}

// runMCP keeps stdout exclusively for JSON-RPC and never prompts for settings.
func runMCP(project string, args []string) error {
	flags := flag.NewFlagSet("workwood mcp", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	dataDir := flags.String("data-dir", "", "default project's local data directory")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("usage: workwood mcp [-p <project>] [--data-dir <path>]")
	}
	s, err := mcpserver.New(mcpserver.Options{Project: project, DataDir: *dataDir})
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return s.Serve(ctx, os.Stdin, os.Stdout)
}

// initLang resolves the active language (env > app setting > $LANG > en) and
// loads the catalog. The app-settings read is best-effort.
func initLang() {
	lang := ""
	if app, _, err := config.LoadApp(); err == nil {
		lang = app.Language
	}
	i18n.Init(i18n.Resolve(lang))
}

// notifyIfOutdated prints a one-line stderr notice when a newer release exists.
func notifyIfOutdated() {
	app, home, err := config.LoadApp()
	if err != nil {
		return
	}
	update.Notify(home, version.Software, app.UpdateCheckEnabled())
}

// extractProjectFlag pulls a -p/--project <path> value out of args (anywhere).
func extractProjectFlag(args []string) (string, []string) {
	project := ""
	out := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-p" || a == "--project":
			if i+1 < len(args) {
				project = args[i+1]
				i++
			}
		case strings.HasPrefix(a, "--project="):
			project = strings.TrimPrefix(a, "--project=")
		case strings.HasPrefix(a, "-p="):
			project = strings.TrimPrefix(a, "-p=")
		default:
			out = append(out, a)
		}
	}
	return project, out
}

// ---- project resolution ---------------------------------------------------

// loadProject locates the super-repo (cwd walk-up or -p path), resolves the data
// dir, and builds the Config + loads its project definition.
func loadProject(projectFlag string) (*models.Config, *models.ProjectDef, error) {
	registered, err := config.ListProjects()
	if err != nil {
		return nil, nil, err
	}
	expectedID := ""
	for _, p := range registered {
		if projectFlag == p.ID {
			expectedID = p.ID
			projectFlag = p.Root
			break
		}
	}
	loc, err := config.LocateProject(projectFlag)
	if err != nil {
		return nil, nil, err
	}
	if expectedID != "" && expectedID != loc.PD.ID {
		return nil, nil, fmt.Errorf("registered project %s has a different identity at %s; register its current location", expectedID, projectFlag)
	}
	// A feature link carries its own data dir, so running from a feature folder
	// works with neither the super-repo as cwd nor $WORKWOOD_DATA set.
	dataDir := loc.DataDir
	if dataDir == "" {
		dataDir = os.Getenv(config.EnvData)
	}
	if dataDir == "" {
		for _, p := range registered {
			if p.ID == loc.PD.ID {
				dataDir = p.DataDir
				break
			}
		}
	}
	if dataDir == "" {
		if dataDir, err = ensureDataDir(); err != nil {
			return nil, nil, err
		}
	}
	cfg, err := config.Build(loc.Root, loc.PD, dataDir)
	if err != nil {
		return nil, nil, err
	}
	cfg.ActiveFeature = loc.ActiveFeature
	return cfg, loc.PD, nil
}

// resolveCfg is loadProject when only the Config is needed.
func resolveCfg(projectFlag string) (*models.Config, error) {
	cfg, _, err := loadProject(projectFlag)
	return cfg, err
}

// ensureDataDir resolves $WORKWOOD_DATA → saved data_dir → an interactive prompt
// (saved back to app settings). Errors when unset and non-interactive.
func ensureDataDir() (string, error) {
	if d := strings.TrimRight(os.Getenv(config.EnvData), "/"); d != "" {
		return d, nil
	}
	app, home, err := config.LoadApp()
	if err != nil {
		return "", err
	}
	if app.DataDir != "" {
		return app.DataDir, nil
	}
	if !interactive() {
		return "", i18n.Err("err.no_data_dir")
	}
	def := defaultDataDir()
	d := prompt(i18n.T("init.data_dir_prompt"), def)
	if abs, e := filepath.Abs(d); e == nil {
		d = abs
	}
	app.DataDir = d
	if err := config.SaveApp(home, app); err != nil {
		return "", err
	}
	return d, nil
}

// defaultDataDir suggests ~/workwood-data (external to ~/.workwood).
func defaultDataDir() string {
	if h, err := os.UserHomeDir(); err == nil {
		return filepath.Join(h, "workwood-data")
	}
	return "./workwood-data"
}

// interactive reports whether stdin is a terminal (so prompting is sensible).
func interactive() bool {
	fi, err := os.Stdin.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

// runTUI launches the interactive editor for the resolved project.
func runTUI(projectFlag string) error {
	cfg, pd, err := loadProject(projectFlag)
	if err != nil {
		return err
	}
	return tui.Run(cfg, pd)
}

// ---- lang -----------------------------------------------------------------

func runLang(args []string) error {
	app, home, err := config.LoadApp()
	if err != nil {
		return err
	}
	if len(args) == 0 {
		fmt.Println(i18n.T("lang.current", i18n.Lang(), strings.Join(i18n.Supported, ", ")))
		return nil
	}
	if !i18n.IsSupported(args[0]) {
		return i18n.Err("err.unknown_lang", args[0], strings.Join(i18n.Supported, ", "))
	}
	lang := i18n.Resolve(args[0])
	app.Language = lang
	if err := config.SaveApp(home, app); err != nil {
		return err
	}
	i18n.Init(lang)
	fmt.Println(i18n.T("lang.set", lang))
	return nil
}

// ---- init -----------------------------------------------------------------

// runInit finds an existing project or scaffolds an explicit directory, updates
// its local setup metadata, and registers the resolved root and data directory.
func runInit(args []string) error {
	pos, flags := splitFlags(args)
	path := "."
	if len(pos) > 0 {
		path = pos[0]
	}
	status, err := config.DetectProject(path, flags["data-dir"])
	if err != nil {
		return err
	}
	dataDir := firstNonEmpty(status.DataDir, flags["data-dir"])
	if dataDir == "" {
		dataDir, err = ensureDataDir()
		if err != nil {
			return err
		}
	}
	result, err := config.InitializeProject(path, dataDir, "")
	if err != nil {
		return err
	}

	root, res := result.Project.Root, result.Initialized
	fmt.Print(i18n.T("init.done", root, filepath.Join(root, config.ProjectDefName), res.ActionsDir, res.ManifestsDir, res.StateFile))
	if result.DefinitionChanged {
		fmt.Println(i18n.T("init.commit_hint", config.ProjectDefName))
	}
	if res.Tracked > 0 {
		fmt.Println(i18n.T("init.tracked", res.Tracked))
	}
	return nil
}

// ---- project (info / rename) ----------------------------------------------

func runProject(projectFlag string, args []string) error {
	sub := "info"
	if len(args) > 0 {
		sub = args[0]
		args = args[1:]
	}
	switch sub {
	case "check":
		pos, flags := splitFlags(args)
		path := projectFlag
		if len(pos) > 0 {
			path = pos[0]
		}
		status, err := config.DetectProject(path, flags["data-dir"])
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		if encodeErr := encoder.Encode(status); encodeErr != nil {
			return encodeErr
		}
		return err
	case "list":
		projects, err := config.ListProjects()
		if err != nil {
			return err
		}
		for _, p := range projects {
			fmt.Printf("%s  %s  %s  %s\n", p.ID, p.Name, p.Root, p.DataDir)
		}
		return nil
	case "register":
		pos, flags := splitFlags(args)
		path := projectFlag
		if len(pos) > 0 {
			path = pos[0]
		}
		if path == "" {
			path = "."
		}
		dataDir := firstNonEmpty(flags["data-dir"], os.Getenv(config.EnvData))
		entry, err := config.RegisterProject(path, dataDir)
		if err != nil {
			return err
		}
		fmt.Printf("%s  %s  %s\n", entry.ID, entry.Root, entry.DataDir)
		return nil
	case "unregister":
		if len(args) != 1 {
			return fmt.Errorf("usage: workwood project unregister <uuid>")
		}
		return config.UnregisterProject(args[0])
	}
	cfg, _, err := loadProject(projectFlag)
	if err != nil {
		return err
	}
	switch sub {
	case "info":
		fmt.Print(i18n.T("project.info", cfg.ProjectName, cfg.ProjectSlug, cfg.ProjectID, cfg.Root, cfg.MainDir, cfg.FeaturesDir, cfg.StateFile))
		return nil
	case "rename":
		if len(args) < 1 {
			return i18n.Err("err.usage_project_rename")
		}
		st, err := config.LoadState(cfg.StateFile)
		if err != nil {
			return err
		}
		st.Project = cfg.ProjectID
		st.Name = args[0]
		if err := config.SaveState(cfg.StateFile, st); err != nil {
			return err
		}
		fmt.Println(i18n.T("project.renamed", args[0]))
		return nil
	default:
		return i18n.Err("err.unknown_project_sub", sub)
	}
}

// ---- repos ----------------------------------------------------------------

func runRepos(projectFlag string, args []string) error {
	cfg, pd, err := loadProject(projectFlag)
	if err != nil {
		return err
	}
	sub := "list"
	if len(args) > 0 {
		sub = args[0]
	}
	switch sub {
	case "pull":
		return repos.Pull(cfg, pd)
	case "list":
		for _, r := range repos.List(cfg, pd) {
			state := i18n.T("repos.state_missing")
			if r.Cloned {
				state = i18n.T("repos.state_cloned")
			}
			branch := r.DefaultBranch
			if branch == "" {
				branch = "-"
			}
			fmt.Printf("%-22s %-10s %s\n", r.Name, branch, state)
		}
		return nil
	case "-h", "--help", "help":
		fmt.Println(i18n.T("repos.usage"))
		return nil
	default:
		return i18n.Err("err.unknown_repos_sub", sub)
	}
}

// ---- actions / targets ----------------------------------------------------

// runAction runs an action against a feature's targets:
//
//	workwood action <name> <feature> [--targets <preset|file>]
//
// With no --targets it uses the feature's persisted working set; otherwise it
// loads the named preset (or a literal YAML file path).
func runAction(projectFlag string, args []string) error {
	pos, flags := splitFlags(args)
	if len(pos) < 1 {
		return i18n.Err("err.usage_action")
	}
	cfg, err := resolveCfg(projectFlag)
	if err != nil {
		return err
	}
	name := pos[0]
	explicit := ""
	if len(pos) >= 2 {
		explicit = pos[1]
	}
	feature, err := featureArg(cfg, explicit, "err.usage_action")
	if err != nil {
		return err
	}

	var override models.Set
	if tf := flags["targets"]; tf != "" {
		override, err = loadTargetsArg(cfg, tf)
		if err != nil {
			return err
		}
	}
	// --init runs the action's Init (bootstrap files in the selected targets).
	if hasFlag(flags, "init") {
		out, err := superfeature.InitAction(cfg, feature, name, override)
		if out != "" {
			fmt.Print(out)
		}
		return err
	}
	return superfeature.RunAction(cfg, feature, name, override)
}

// loadTargetsArg resolves a --targets value: a preset name (in the targets dir)
// or, when it looks like a path, a literal YAML file of key→path.
func loadTargetsArg(cfg *models.Config, arg string) (models.Set, error) {
	if strings.ContainsAny(arg, "/.") {
		data, err := os.ReadFile(arg)
		if err != nil {
			return nil, err
		}
		set := models.Set{}
		if err := yaml.Unmarshal(data, &set); err != nil {
			return nil, i18n.Errw(err, "err.parse_file", arg)
		}
		return set, nil
	}
	return targetcfg.LoadPreset(cfg, arg)
}

func runActions(projectFlag string) error {
	cfg, err := resolveCfg(projectFlag)
	if err != nil {
		return err
	}
	as, needChmod := action.Scan(cfg)
	if len(needChmod) > 0 {
		fmt.Println(i18n.T("actions.need_chmod", strings.Join(needChmod, ", ")))
	}
	if len(as) == 0 {
		fmt.Println(i18n.T("actions.none", cfg.ActionsDir))
		return nil
	}
	for _, a := range as {
		if a.Description != "" {
			fmt.Printf("%-20s %s\n", a.Name, a.Description)
		} else {
			fmt.Println(a.Name)
		}
	}
	return nil
}

// runTargets lists saved presets or shows a feature's working set.
func runTargets(projectFlag string, args []string) error {
	cfg, err := resolveCfg(projectFlag)
	if err != nil {
		return err
	}
	sub := "list"
	if len(args) > 0 {
		sub = args[0]
		args = args[1:]
	}
	switch sub {
	case "list":
		presets := targetcfg.ListPresets(cfg)
		if len(presets) == 0 {
			fmt.Println(i18n.T("targets.empty"))
			return nil
		}
		fmt.Println(i18n.T("targets.list_header"))
		for _, p := range presets {
			fmt.Println("  " + p)
		}
		return nil
	case "show":
		feat, err := featureArg(cfg, firstPos(args), "err.usage_targets_show")
		if err != nil {
			return err
		}
		m, err := manifest.Load(cfg.ManifestPath(feat))
		if err != nil {
			return err
		}
		pd, err := projectdef.Load(cfg.ProjectDef)
		if err != nil {
			return err
		}
		set, err := targetcfg.Working(cfg, pd, m)
		if err != nil {
			return err
		}
		if len(set) == 0 {
			fmt.Println(i18n.T("targets.empty"))
			return nil
		}
		fmt.Println(i18n.T("targets.show_header", feat))
		keys := make([]string, 0, len(set))
		for k := range set {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Printf("  %-24s %s\n", k, set[k])
		}
		return nil
	case "generate", "gen":
		feat, err := featureArg(cfg, firstPos(args), "err.usage_targets_generate")
		if err != nil {
			return err
		}
		m, err := manifest.Load(cfg.ManifestPath(feat))
		if err != nil {
			return err
		}
		pd, err := projectdef.Load(cfg.ProjectDef)
		if err != nil {
			return err
		}
		if err := targetcfg.SavePreset(cfg, feat, targetcfg.CleanSet(cfg, pd, m)); err != nil {
			return err
		}
		fmt.Println(i18n.T("targets.generated", feat, targetcfg.PresetPath(cfg, feat)))
		return nil
	default:
		return i18n.Err("err.unknown_targets_sub", sub)
	}
}

// ---- super-feature --------------------------------------------------------

func runFeature(projectFlag string, args []string) error {
	sub := ""
	if len(args) > 0 {
		sub = args[0]
		args = args[1:]
	}
	if sub == "" || sub == "-h" || sub == "--help" || sub == "help" {
		featureUsage()
		return nil
	}

	cfg, err := resolveCfg(projectFlag)
	if err != nil {
		return err
	}

	switch sub {
	case "list":
		feats, err := superfeature.List(cfg)
		if err != nil {
			return err
		}
		if len(feats) == 0 {
			fmt.Println(i18n.T("feature.none"))
			return nil
		}
		st, _ := config.LoadState(cfg.StateFile)
		for _, f := range feats {
			name := f.Feature
			if st != nil {
				if fs, ok := st.FeatureByUUID(f.ID); ok {
					name = fs.DisplayName()
				}
			}
			fmt.Printf("%-24s %s\n", name, i18n.T("feature.list_meta", len(f.Worktrees), f.Description))
		}
		return nil

	case "create":
		pos, flags := splitFlags(args)
		if len(pos) < 1 {
			return i18n.Err("err.usage_sf_create")
		}
		desc := ""
		if len(pos) > 1 {
			desc = pos[1]
		}
		shorthand := flags["shorthand"]
		if err := superfeature.Create(cfg, pos[0], shorthand, desc); err != nil {
			return err
		}
		if shorthand == "" {
			shorthand = manifest.DefaultShorthand(pos[0])
		}
		fmt.Println(i18n.T("feature.created", cfg.ManifestPath(pos[0]), shorthand))
		fmt.Println(i18n.T("feature.add_hint", pos[0], shorthand))
		return nil

	case "rename":
		pos, _ := splitFlags(args)
		if len(pos) < 2 {
			return i18n.Err("err.usage_sf_rename")
		}
		return renameFeature(cfg, pos[0], pos[1])

	case "add":
		pd, err := projectdef.Load(cfg.ProjectDef)
		if err != nil {
			return err
		}
		pos, flags := splitFlags(args)
		if len(pos) < 3 {
			return i18n.Err("err.usage_sf_add")
		}
		from := firstNonEmpty(flags["from"], flags["source"])
		if from == "" && len(pos) > 3 {
			from = pos[3]
		}
		omit := hasFlag(flags, "no-feature-prefix", "strip-feature")
		spec := superfeature.AddSpec{
			Repo: pos[1], Sub: pos[2], From: from, OmitFeaturePrefix: omit,
			ExistingBranch: hasFlag(flags, "existing"),
			BaseSource:     flags["base-source"],
		}
		if !spec.ExistingBranch && spec.BaseSource == "" {
			status, inspectErr := superfeature.InspectBaseContext(context.Background(), cfg, pd, spec)
			if inspectErr != nil {
				return inspectErr
			}
			spec.BaseSource, err = promptBaseSource(bufio.NewScanner(os.Stdin), os.Stdout, status)
			if err != nil {
				return err
			}
		}
		wt, err := superfeature.Add(cfg, pd, pos[0], spec)
		if err != nil {
			return err
		}
		fmt.Println(i18n.T("feature.added", wt.Repo, wt.Branch, wt.Path))
		return nil

	case "up":
		pos, _ := splitFlags(args)
		feat, err := featureArg(cfg, firstPos(pos), "err.usage_sf_up")
		if err != nil {
			return err
		}
		sc := bufio.NewScanner(os.Stdin)
		if err := checkFolderNames(cfg, feat, sc, os.Stdout); err != nil {
			return err
		}
		log, err := superfeature.Up(cfg, feat, func(repo, branch string) bool {
			return confirmNewBranch(sc, os.Stdout, repo, branch)
		})
		if err != nil {
			return err
		}
		printLines(log)
		return nil

	case "status":
		pos, _ := splitFlags(args)
		feat, err := featureArg(cfg, firstPos(pos), "err.usage_sf_status")
		if err != nil {
			return err
		}
		_, rows, err := superfeature.Status(cfg, feat)
		if err != nil {
			return err
		}
		fmt.Println(i18n.T("feature.status_header", feat))
		if len(rows) == 0 {
			fmt.Println(i18n.T("feature.status_none"))
			return nil
		}
		for _, r := range rows {
			if r.CheckedOut {
				fmt.Printf("  %-22s %-30s %s\n", r.Repo, r.Branch, i18n.T("feature.status_dirty", r.StatusLine, r.DirtyCount))
			} else {
				fmt.Printf("  %-22s %-30s %s\n", r.Repo, r.Branch, i18n.T("feature.status_absent"))
			}
		}
		return nil

	case "remove":
		pos, flags := splitFlags(args)
		if len(pos) < 2 {
			return i18n.Err("err.usage_sf_remove")
		}
		spec := superfeature.RemoveSpec{Repo: pos[1], PruneBranch: hasFlag(flags, "prune-branch", "prune-branches")}
		if len(pos) > 2 {
			spec.Sub = pos[2]
		}
		msg, err := superfeature.Remove(cfg, pos[0], spec)
		if err != nil {
			return err
		}
		fmt.Println(msg)
		return nil

	case "down":
		pos, _ := splitFlags(args)
		feat, err := featureArg(cfg, firstPos(pos), "err.usage_sf_down")
		if err != nil {
			return err
		}
		log, err := superfeature.Down(cfg, feat)
		if err != nil {
			return err
		}
		printLines(log)
		fmt.Println(i18n.T("feature.down_done", feat))
		return nil

	case "delete":
		pos, flags := splitFlags(args)
		feat, err := featureArg(cfg, firstPos(pos), "err.usage_sf_delete")
		if err != nil {
			return err
		}
		prune := hasFlag(flags, "prune-branch", "prune-branches")
		if err := superfeature.Delete(cfg, feat, prune); err != nil {
			return err
		}
		if prune {
			fmt.Println(i18n.T("feature.deleted_pruned", feat))
		} else {
			fmt.Println(i18n.T("feature.deleted", feat))
		}
		return nil

	case "relink", "verify":
		// Validate the feature folder's back-link (.workwood/link.yml) and rewrite
		// it if missing / out of date. With no feature it checks them all.
		pos, _ := splitFlags(args)
		slugs := []string{}
		if f := firstPos(pos); f != "" {
			slugs = []string{f}
		} else if cfg.ActiveFeature != "" {
			slugs = []string{cfg.ActiveFeature}
		} else {
			st, err := config.LoadState(cfg.StateFile)
			if err != nil {
				return err
			}
			for _, fs := range st.Features {
				slugs = append(slugs, fs.Slug)
			}
			sort.Strings(slugs)
		}
		if len(slugs) == 0 {
			fmt.Println(i18n.T("feature.none"))
			return nil
		}
		for _, slug := range slugs {
			regen, err := config.EnsureFeatureLink(cfg, slug)
			if err != nil {
				return err
			}
			if regen {
				fmt.Println(i18n.T("feature.relinked", slug, cfg.FeatureLinkPath(slug)))
			} else {
				fmt.Println(i18n.T("feature.relink_ok", slug))
			}
		}
		return nil

	case "doctor", "reconcile":
		return runDoctor(cfg, args)

	default:
		return i18n.Err("err.unknown_sf_sub", sub)
	}
}

// runDoctor reports + resolves a feature's manifest↔disk desyncs: orphan worktrees
// (on disk, untracked) and missing ones (tracked, no checkout). With no flags it
// asks per item; --adopt / --remove-orphans / --rebuild / --drop run non-interactively.
func runDoctor(cfg *models.Config, args []string) error {
	pos, flags := splitFlags(args)
	feat, err := featureArg(cfg, firstPos(pos), "err.usage_sf_doctor")
	if err != nil {
		return err
	}
	pd, err := projectdef.Load(cfg.ProjectDef)
	if err != nil {
		return err
	}
	d, err := superfeature.Diagnose(cfg, pd, feat)
	if err != nil {
		return err
	}
	if d.OK() {
		fmt.Println(i18n.T("doctor.ok", feat))
		return nil
	}
	fmt.Println(i18n.T("doctor.header", feat))
	for _, o := range d.Orphans {
		fmt.Println("  " + i18n.T("doctor.orphan", nz(o.Repo), nz(o.Branch), o.Abs))
	}
	for _, w := range d.Missing {
		fmt.Println("  " + i18n.T("doctor.missing", w.Repo, w.Branch, w.Path))
	}

	adopt := hasFlag(flags, "adopt")
	removeOrph := hasFlag(flags, "remove-orphans")
	rebuild := hasFlag(flags, "rebuild")
	drop := hasFlag(flags, "drop")
	bulk := adopt || removeOrph || rebuild || drop

	sc := bufio.NewScanner(os.Stdin)
	ask := func(text, def string, valid ...string) string {
		fmt.Print(text)
		if !sc.Scan() {
			fmt.Println()
			return def
		}
		c := strings.ToLower(strings.TrimSpace(sc.Text()))
		if c == "" {
			return def
		}
		for _, v := range valid {
			if c == v {
				return c
			}
		}
		return def
	}
	// Gather a decision per item, then apply them all via superfeature.Reconcile
	// (shared with the TUI's doctor screen).
	var plan superfeature.ReconcilePlan
	for _, o := range d.Orphans {
		c := "s"
		switch {
		case adopt:
			c = "a"
		case removeOrph:
			c = "r"
		case !bulk:
			c = ask(i18n.T("doctor.ask_orphan", nz(o.Repo), nz(o.Branch)), "s", "a", "r", "s")
		}
		switch c {
		case "a":
			plan.AdoptOrphans = append(plan.AdoptOrphans, o)
		case "r":
			plan.RemoveOrphans = append(plan.RemoveOrphans, o)
		}
	}
	for _, w := range d.Missing {
		c := "s"
		switch {
		case rebuild:
			c = "b"
		case drop:
			c = "d"
		case !bulk:
			c = ask(i18n.T("doctor.ask_missing", w.Repo, w.Branch), "s", "b", "d", "s")
		}
		switch c {
		case "b":
			plan.RebuildMissing = append(plan.RebuildMissing, w)
		case "d":
			plan.DropMissing = append(plan.DropMissing, w)
		}
	}
	for _, oc := range superfeature.Reconcile(cfg, feat, plan) {
		if oc.Err != nil {
			fmt.Println("  " + oc.Err.Error())
		} else {
			fmt.Println("  " + oc.Msg)
		}
	}
	return nil
}

// nz returns s, or "?" when empty (for reporting an orphan whose repo/branch
// couldn't be determined).
func nz(s string) string {
	if s == "" {
		return "?"
	}
	return s
}

// renameFeature sets a super-feature's local active_name (keyed by its UUID),
// leaving the slug — and therefore the manifest filename + branches — untouched.
func renameFeature(cfg *models.Config, slug, newName string) error {
	m, err := manifest.Load(cfg.ManifestPath(slug))
	if err != nil {
		return err
	}
	if m.ID == "" {
		return i18n.Err("err.feature_not_adopted", slug)
	}
	st, err := config.LoadState(cfg.StateFile)
	if err != nil {
		return err
	}
	st.EnsureFeature(m.ID, slug)
	f := st.Features[m.ID]
	f.Name = newName
	st.Features[m.ID] = f
	if err := config.SaveState(cfg.StateFile, st); err != nil {
		return err
	}
	fmt.Println(i18n.T("feature.renamed", slug, newName))
	return nil
}

// ---- shared CLI helpers ---------------------------------------------------

// splitFlags separates --flag / --flag=value / --flag value tokens from
// positional args. Boolean flags map to "".
func splitFlags(args []string) (pos []string, flags map[string]string) {
	flags = map[string]string{}
	valueFlags := map[string]bool{"from": true, "source": true, "base-source": true, "name": true, "targets": true, "shorthand": true, "data-dir": true}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if len(a) > 2 && a[:2] == "--" {
			name := a[2:]
			if eq := strings.IndexByte(name, '='); eq >= 0 {
				flags[name[:eq]] = name[eq+1:]
				continue
			}
			if valueFlags[name] && i+1 < len(args) {
				flags[name] = args[i+1]
				i++
				continue
			}
			flags[name] = ""
			continue
		}
		pos = append(pos, a)
	}
	return pos, flags
}

func hasFlag(flags map[string]string, names ...string) bool {
	for _, n := range names {
		if _, ok := flags[n]; ok {
			return true
		}
	}
	return false
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// featureArg resolves which feature a command acts on: the explicit positional if
// given, else the active feature implied by running inside a feature folder
// (cfg.ActiveFeature). Errors with usageKey when neither is available.
func featureArg(cfg *models.Config, explicit, usageKey string) (string, error) {
	if explicit != "" {
		return explicit, nil
	}
	if cfg.ActiveFeature != "" {
		return cfg.ActiveFeature, nil
	}
	return "", i18n.Err(usageKey)
}

// firstPos returns the first positional arg, or "" when there are none.
func firstPos(pos []string) string {
	if len(pos) > 0 {
		return pos[0]
	}
	return ""
}

// confirmNewBranch asks whether to create a new local branch for a worktree whose
// branch is on neither the local repo nor origin (used by `sf up`). Defaults to
// yes; a non-interactive stdin (EOF) also yields yes, so scripts aren't blocked.
func confirmNewBranch(sc *bufio.Scanner, out io.Writer, repo, branch string) bool {
	fmt.Fprint(out, i18n.T("sf.up_no_remote", branch, repo))
	if !sc.Scan() {
		fmt.Fprintln(out)
		return true
	}
	switch strings.ToLower(strings.TrimSpace(sc.Text())) {
	case "", "y", "yes":
		return true
	default:
		return false
	}
}

type baseSourceChoice struct {
	value string
	label string
}

// promptBaseSource asks which freshly inspected ref should seed a new branch.
// EOF selects the first available option, keeping non-interactive CLI use stable.
func promptBaseSource(sc *bufio.Scanner, out io.Writer, status superfeature.BaseStatus) (string, error) {
	choices := []baseSourceChoice{}
	if status.OriginExists {
		choices = append(choices, baseSourceChoice{superfeature.BaseSourceOrigin, i18n.T("sf.base_origin", status.Base)})
	}
	if status.LocalExists {
		choices = append(choices, baseSourceChoice{superfeature.BaseSourceLocal, i18n.T("sf.base_local", status.Base)})
	}
	if status.OriginExists {
		choices = append(choices, baseSourceChoice{superfeature.BaseSourcePull, i18n.T("sf.base_pull", status.Base)})
	}
	if len(choices) == 0 {
		return "", i18n.Err("err.no_base_ref", status.Base)
	}
	fmt.Fprintln(out, i18n.T("sf.base_status", status.Base, status.LocalAhead, status.LocalBehind))
	for i, choice := range choices {
		fmt.Fprintf(out, "  %d) %s\n", i+1, choice.label)
	}
	for {
		fmt.Fprint(out, i18n.T("sf.base_prompt", 1))
		if !sc.Scan() {
			fmt.Fprintln(out)
			return choices[0].value, nil
		}
		answer := strings.TrimSpace(sc.Text())
		if answer == "" {
			return choices[0].value, nil
		}
		for i, choice := range choices {
			if answer == fmt.Sprint(i+1) || strings.EqualFold(answer, choice.value) {
				return choice.value, nil
			}
		}
		fmt.Fprintln(out, i18n.T("sf.base_invalid"))
	}
}

// checkFolderNames collects every choice before applying any rename or drop.
// EOF and cancellation leave the manifest and all checkouts unchanged.
func checkFolderNames(cfg *models.Config, name string, sc *bufio.Scanner, out io.Writer) error {
	changes, err := superfeature.CheckFolderNames(cfg, name)
	if err != nil {
		return err
	}
	var decisions []superfeature.FolderDecision
	for _, change := range changes {
		fmt.Fprintln(out, i18n.T("folders.paths", change.Worktree.Path, change.Path))
		if change.Missing {
			fmt.Fprintln(out, i18n.T("folders.missing"))
		}
		for {
			fmt.Fprint(out, i18n.T("folders.cli_prompt", change.Worktree.Repo, change.Worktree.Branch))
			if !sc.Scan() {
				fmt.Fprintln(out)
				if err := sc.Err(); err != nil {
					return err
				}
				return i18n.Err("err.folder_cancelled")
			}
			answer := strings.ToLower(strings.TrimSpace(sc.Text()))
			switch answer {
			case "", "y", "yes", "n", "no":
				decisions = append(decisions, superfeature.FolderDecision{FolderChange: change, Rename: answer != "n" && answer != "no"})
			case "c", "cancel", "q":
				return i18n.Err("err.folder_cancelled")
			default:
				fmt.Fprintln(out, i18n.T("folders.cli_choices"))
				continue
			}
			break
		}
	}
	log, err := superfeature.ApplyFolderNames(cfg, name, decisions)
	for _, line := range log {
		fmt.Fprintln(out, line)
	}
	return err
}

// prompt asks for a value on stdin, returning def if the line is empty.
func prompt(label, def string) string {
	fmt.Print(i18n.T("cli.prompt", label, def))
	sc := bufio.NewScanner(os.Stdin)
	if !sc.Scan() {
		fmt.Println()
		return def
	}
	v := strings.TrimSpace(sc.Text())
	if v == "" {
		return def
	}
	return v
}

func printLines(lines []string) {
	for _, l := range lines {
		fmt.Println(l)
	}
}

func usage()        { fmt.Print(i18n.T("usage.main")) }
func featureUsage() { fmt.Print(i18n.T("usage.feature")) }

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, i18n.T("cli.error_prefix"), err)
		os.Exit(1)
	}
}
