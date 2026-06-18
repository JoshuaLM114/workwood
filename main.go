// Command workwood orchestrates git worktrees across many repos, organised into
// "super-features" that can span repos and hold multiple branches of the same
// repo (which submodules can't).
//
// workwood is a GLOBAL CLI. It operates on PROJECTS you register in
// ~/.workwood/config.yaml: each project is a team "super-repo" holding a
// workwood.yaml (its repo definitions) plus super-features/<name>.yaml manifests
// (committed + shared). Your registry entry records where YOU keep the base
// clones and feature worktrees on disk.
//
// User-facing text is loaded from the message catalog (package i18n) so the CLI
// can run in English or Japanese; pick with `workwood lang ja` or $WORKWOOD_LANG.
package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/JoshuaLM114/workwood/config"
	"github.com/JoshuaLM114/workwood/i18n"
	"github.com/JoshuaLM114/workwood/plugin"
	"github.com/JoshuaLM114/workwood/plugins"
	"github.com/JoshuaLM114/workwood/projectdef"
	"github.com/JoshuaLM114/workwood/repos"
	"github.com/JoshuaLM114/workwood/sandbox"
	"github.com/JoshuaLM114/workwood/superfeature"
	"github.com/JoshuaLM114/workwood/targets"
	"github.com/JoshuaLM114/workwood/tui"
	"github.com/JoshuaLM114/workwood/version"
)

func main() {
	initLang()    // resolve + load the message catalog before any output
	seedPlugins() // best-effort: populate ~/.workwood/plugins with the defaults

	args := os.Args[1:]
	projectFlag, args := extractProjectFlag(args)

	cmd := ""
	if len(args) > 0 {
		cmd = args[0]
		args = args[1:]
	}

	switch cmd {
	case "init":
		must(runInit(args))
	case "sandbox":
		must(runSandbox(args))
	case "lang", "language":
		must(runLang(args))
	case "project", "projects":
		must(runProject(args))
	case "repos":
		must(runRepos(projectFlag, args))
	case "super-feature", "sf", "feature":
		must(runFeature(projectFlag, args))
	case "compose":
		must(runCompose(projectFlag, args))
	case "plugins":
		must(runPlugins(projectFlag))
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

// initLang resolves the active language (env > config > $LANG > en) and loads the
// catalog. The registry read is best-effort so a broken config still shows text.
func initLang() {
	cfgLang := ""
	if reg, _, err := config.LoadRegistry(); err == nil {
		cfgLang = reg.Language
	}
	i18n.Init(i18n.Resolve(cfgLang))
}

// seedPlugins writes the bundled default plugins into ~/.workwood/plugins on
// first run (never clobbering an edited one). Best-effort.
func seedPlugins() {
	home, err := config.Home()
	if err != nil {
		return
	}
	_, _ = plugins.Seed(config.GlobalPluginsDir(home))
}

// extractProjectFlag pulls a -p/--project value out of args (anywhere), so it
// can precede or follow the subcommand. Returns the value and the args without it.
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

// runTUI launches the interactive editor for the selected project.
func runTUI(projectFlag string) error {
	cfg, pd, err := loadProject(projectFlag)
	if err != nil {
		return err
	}
	return tui.Run(cfg, pd)
}

// loadProject resolves the selected project and loads its definition.
func loadProject(projectFlag string) (*config.Config, *projectdef.File, error) {
	cfg, err := config.Resolve(projectFlag)
	if err != nil {
		return nil, nil, err
	}
	pd, err := projectdef.Load(cfg.ProjectDef)
	if err != nil {
		return nil, nil, err
	}
	return cfg, pd, nil
}

// ---- lang -----------------------------------------------------------------

func runLang(args []string) error {
	reg, home, err := config.LoadRegistry()
	if err != nil {
		return err
	}
	if len(args) == 0 {
		fmt.Println(i18n.T("lang.current", i18n.Lang(), strings.Join(i18n.Supported, ", ")))
		return nil
	}
	lang := i18n.Resolve(args[0])
	if !i18n.IsSupported(args[0]) {
		return i18n.Err("err.unknown_lang", args[0], strings.Join(i18n.Supported, ", "))
	}
	reg.Language = lang
	if err := config.SaveRegistry(home, reg); err != nil {
		return err
	}
	i18n.Init(lang) // confirm in the newly selected language
	fmt.Println(i18n.T("lang.set", lang))
	return nil
}

// ---- sandbox --------------------------------------------------------------

func runSandbox(args []string) error {
	pos, _ := splitFlags(args)
	dir := "./workwood-sandbox"
	if len(pos) > 0 {
		dir = pos[0]
	}
	res, err := sandbox.Create(dir)
	if err != nil {
		return err
	}
	fmt.Print(i18n.T("sandbox.created", res.Dir))
	fmt.Print(i18n.T("sandbox.activate", res.Activate))
	fmt.Print(i18n.T("sandbox.explore"))
	fmt.Print(i18n.T("sandbox.walkthrough", filepath.Join(res.Dir, "README.md")))
	return nil
}

// ---- init / project registry ---------------------------------------------

func runInit(args []string) error {
	pos, flags := splitFlags(args)
	path := "."
	if len(pos) > 0 {
		path = pos[0]
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	name := flags["name"]
	if name == "" {
		name = filepath.Base(abs)
	}

	// Scaffold a starter workwood.yaml + super-features/ if the dir has none yet.
	defPath := filepath.Join(abs, config.ProjectDefName)
	if _, err := os.Stat(defPath); os.IsNotExist(err) {
		if err := os.MkdirAll(filepath.Join(abs, config.ManifestsDirName), 0o755); err != nil {
			return err
		}
		skeleton := &projectdef.File{Org: "CHANGE_ME", Repos: []projectdef.Repo{}}
		if err := projectdef.Save(defPath, skeleton); err != nil {
			return err
		}
		fmt.Println(i18n.T("init.scaffolded", defPath))
	} else if err != nil {
		return err
	}

	reg, home, err := config.LoadRegistry()
	if err != nil {
		return err
	}
	existing := reg.Projects[name]
	mainDir := firstNonEmpty(flags["main"], existing.MainDir)
	featuresDir := firstNonEmpty(flags["features"], existing.FeaturesDir)
	if mainDir == "" {
		mainDir = prompt(i18n.T("init.prompt_main"), filepath.Join(abs, ".workwood-data", "main"))
	}
	if featuresDir == "" {
		featuresDir = prompt(i18n.T("init.prompt_features"), filepath.Join(abs, ".workwood-data", "features"))
	}
	if mainDir == "" || featuresDir == "" {
		return i18n.Err("err.dirs_required")
	}

	mainAbs, _ := filepath.Abs(mainDir)
	featAbs, _ := filepath.Abs(featuresDir)
	reg.Projects[name] = config.ProjectEntry{Path: abs, MainDir: mainAbs, FeaturesDir: featAbs}
	if reg.DefaultProject == "" {
		reg.DefaultProject = name
	}
	if err := config.SaveRegistry(home, reg); err != nil {
		return err
	}
	fmt.Print(i18n.T("init.registered", name, abs, mainAbs, featAbs))
	if reg.DefaultProject == name {
		fmt.Println(i18n.T("init.set_default"))
	}
	fmt.Println(i18n.T("init.next", name))
	return nil
}

func runProject(args []string) error {
	sub := "list"
	if len(args) > 0 {
		sub = args[0]
		args = args[1:]
	}
	reg, home, err := config.LoadRegistry()
	if err != nil {
		return err
	}
	switch sub {
	case "list":
		if len(reg.Projects) == 0 {
			fmt.Println(i18n.T("project.none"))
			return nil
		}
		for name, e := range reg.Projects {
			marker := "  "
			if name == reg.DefaultProject {
				marker = "* "
			}
			fmt.Printf("%s%-20s %s\n", marker, name, e.Path)
			fmt.Print(i18n.T("project.list_paths", e.MainDir, e.FeaturesDir))
		}
		return nil
	case "use":
		if len(args) < 1 {
			return i18n.Err("err.usage_project_use")
		}
		if _, ok := reg.Projects[args[0]]; !ok {
			return i18n.Err("err.unknown_project", args[0])
		}
		reg.DefaultProject = args[0]
		if err := config.SaveRegistry(home, reg); err != nil {
			return err
		}
		fmt.Println(i18n.T("project.default_set", args[0]))
		return nil
	case "remove", "rm":
		if len(args) < 1 {
			return i18n.Err("err.usage_project_remove")
		}
		if _, ok := reg.Projects[args[0]]; !ok {
			return i18n.Err("err.unknown_project", args[0])
		}
		delete(reg.Projects, args[0])
		if reg.DefaultProject == args[0] {
			reg.DefaultProject = ""
		}
		if err := config.SaveRegistry(home, reg); err != nil {
			return err
		}
		fmt.Println(i18n.T("project.removed", args[0]))
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

// ---- compose / plugins ----------------------------------------------------

func runCompose(projectFlag string, args []string) error {
	pos, flags := splitFlags(args)
	if len(pos) < 2 {
		return i18n.Err("err.usage_compose")
	}
	cfg, err := config.Resolve(projectFlag)
	if err != nil {
		return err
	}
	mode := plugin.ModeRun
	if hasFlag(flags, "init") {
		mode = plugin.ModeInit
	}
	return superfeature.Compose(cfg, pos[1], pos[0], mode)
}

func runPlugins(projectFlag string) error {
	cfg, err := config.Resolve(projectFlag)
	if err != nil {
		return err
	}
	ps := superfeature.Plugins(cfg)
	if len(ps) == 0 {
		fmt.Println(i18n.T("plugins.none", cfg.GlobalPluginsDir, cfg.ProjectPluginsDir))
		return nil
	}
	for _, p := range ps {
		fmt.Println(p)
	}
	return nil
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

	cfg, err := config.Resolve(projectFlag)
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
		for _, f := range feats {
			fmt.Printf("%-24s %s\n", f.Feature, i18n.T("feature.list_meta", len(f.Worktrees), f.Description))
		}
		return nil

	case "create":
		pos, _ := splitFlags(args)
		if len(pos) < 1 {
			return i18n.Err("err.usage_sf_create")
		}
		desc := ""
		if len(pos) > 1 {
			desc = pos[1]
		}
		if err := superfeature.Create(cfg, pos[0], desc); err != nil {
			return err
		}
		fmt.Println(i18n.T("feature.created", cfg.ManifestPath(pos[0])))
		fmt.Println(i18n.T("feature.add_hint", pos[0], pos[0]))
		return nil

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
		wt, err := superfeature.Add(cfg, pd, pos[0], superfeature.AddSpec{Repo: pos[1], Sub: pos[2], From: from, OmitFeaturePrefix: omit})
		if err != nil {
			return err
		}
		fmt.Println(i18n.T("feature.added", wt.Repo, wt.Branch, wt.Path))
		return nil

	case "up":
		pos, _ := splitFlags(args)
		if len(pos) < 1 {
			return i18n.Err("err.usage_sf_up")
		}
		log, err := superfeature.Up(cfg, pos[0])
		if err != nil {
			return err
		}
		printLines(log)
		return nil

	case "status":
		pos, _ := splitFlags(args)
		if len(pos) < 1 {
			return i18n.Err("err.usage_sf_status")
		}
		_, rows, err := superfeature.Status(cfg, pos[0])
		if err != nil {
			return err
		}
		fmt.Println(i18n.T("feature.status_header", pos[0]))
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

	case "target":
		return runTarget(cfg, args)

	case "down":
		pos, _ := splitFlags(args)
		if len(pos) < 1 {
			return i18n.Err("err.usage_sf_down")
		}
		log, err := superfeature.Down(cfg, pos[0])
		if err != nil {
			return err
		}
		printLines(log)
		fmt.Println(i18n.T("feature.down_done", pos[0]))
		return nil

	case "delete":
		pos, flags := splitFlags(args)
		if len(pos) < 1 {
			return i18n.Err("err.usage_sf_delete")
		}
		prune := hasFlag(flags, "prune-branch", "prune-branches")
		if err := superfeature.Delete(cfg, pos[0], prune); err != nil {
			return err
		}
		if prune {
			fmt.Println(i18n.T("feature.deleted_pruned", pos[0]))
		} else {
			fmt.Println(i18n.T("feature.deleted", pos[0]))
		}
		return nil

	default:
		return i18n.Err("err.unknown_sf_sub", sub)
	}
}

// ---- target (add/remove/clear/list, with bulk) ----------------------------

func runTarget(cfg *config.Config, args []string) error {
	if len(args) == 0 {
		return i18n.Err("err.usage_target")
	}
	verb, rest := args[0], args[1:]
	switch verb {
	case "list", "ls":
		pos, _ := splitFlags(rest)
		if len(pos) < 1 {
			return i18n.Err("err.usage_target_list")
		}
		st, err := superfeature.LoadState(cfg, pos[0])
		if err != nil {
			return err
		}
		repos, err := superfeature.Repos(cfg, pos[0])
		if err != nil {
			return err
		}
		fmt.Println(i18n.T("target.list_header", pos[0]))
		for _, repo := range repos {
			fmt.Printf("  %-22s %s\n", repo, targets.LabelList(st.TargetsFor(repo), pos[0]))
		}
		return nil
	case "clear":
		pos, _ := splitFlags(rest)
		if len(pos) < 2 {
			return i18n.Err("err.usage_target_clear")
		}
		if err := superfeature.ClearTarget(cfg, pos[0], pos[1]); err != nil {
			return err
		}
		fmt.Println(i18n.T("target.cleared", pos[1]))
		return nil
	case "add", "remove", "rm":
		return targetAddRemove(cfg, verb, rest)
	default:
		return i18n.Err("err.unknown_target_sub", verb)
	}
}

// targetAddRemove parses `[--all | --repo r ...] <name> [<repo>] <setup> [wt-path]`
// and adds/removes the setup on each selected repo.
func targetAddRemove(cfg *config.Config, verb string, args []string) error {
	all := false
	var repoFlags, pos []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--all":
			all = true
		case a == "--repo":
			if i+1 < len(args) {
				repoFlags = append(repoFlags, args[i+1])
				i++
			}
		case strings.HasPrefix(a, "--repo="):
			repoFlags = append(repoFlags, strings.TrimPrefix(a, "--repo="))
		default:
			pos = append(pos, a)
		}
	}
	usageErr := i18n.Err("err.usage_target_addremove", verb)
	if len(pos) < 1 {
		return usageErr
	}
	name, rest := pos[0], pos[1:]

	var repos []string
	switch {
	case all:
		rs, err := superfeature.Repos(cfg, name)
		if err != nil {
			return err
		}
		repos = rs
	case len(repoFlags) > 0:
		repos = repoFlags
	default:
		if len(rest) < 1 {
			return usageErr
		}
		repos = []string{rest[0]}
		rest = rest[1:]
	}
	if len(rest) < 1 {
		return usageErr
	}
	src := rest[0]
	if src == "auto" {
		return i18n.Err("err.target_auto_clear")
	}
	t := targets.Target{Source: targets.Source(src)}
	if len(rest) > 1 {
		t.Worktree = rest[1]
	}
	if len(repos) == 0 {
		return i18n.Err("err.no_repos_selected", name)
	}
	for _, repo := range repos {
		if verb == "add" {
			added, err := superfeature.AddTarget(cfg, name, repo, t)
			if err != nil {
				return err
			}
			if added {
				fmt.Println(i18n.T("target.added", repo, t.Label(name)))
			} else {
				fmt.Println(i18n.T("target.exists", repo, t.Label(name)))
			}
		} else {
			removed, err := superfeature.RemoveTarget(cfg, name, repo, t)
			if err != nil {
				return err
			}
			if removed {
				fmt.Println(i18n.T("target.removed", repo, t.Label(name)))
			} else {
				fmt.Println(i18n.T("target.none", repo, t.Label(name)))
			}
		}
	}
	return nil
}

// ---- shared CLI helpers ---------------------------------------------------

// splitFlags separates --flag / --flag=value / --flag value tokens from
// positional args. Boolean flags map to "".
func splitFlags(args []string) (pos []string, flags map[string]string) {
	flags = map[string]string{}
	valueFlags := map[string]bool{"from": true, "source": true, "main": true, "features": true, "name": true}
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
