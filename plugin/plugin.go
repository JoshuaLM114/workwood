// Package plugin hands a super-feature's "active context" to a plugin script and
// runs it.
//
// A plugin is just an executable SCRIPT (any language). workwood never composes
// behaviour itself — it only resolves each repo to a target + source dir and
// hands that to the plugin. A plugin is two halves:
//   - a PARENT script (this package finds + execs it), discovered from the
//     project's plugins/ dir first, then the global ~/.workwood/plugins/ dir
//     (project overrides global by name), and
//   - a CHILD script in each referenced repo's .workwood/ folder, which the
//     parent runs per repo and composes the results of.
//
// Run resolves every repo in a feature to a target and a source dir, writes that
// as a TSV context file, and execs the parent — which sources whatever child
// scripts it needs. The built-in targets are ignore | main | worktree; any other
// value (e.g. "deploy") is user-defined and handed through verbatim for the
// repo's .workwood child to interpret. A plugin may also run in MODE=init, where
// (by convention) the parent scaffolds a starter child into each repo's
// .workwood/ folder.
package plugin

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/JoshuaLM114/workwood/config"
	"github.com/JoshuaLM114/workwood/i18n"
	"github.com/JoshuaLM114/workwood/manifest"
	"github.com/JoshuaLM114/workwood/targets"
)

// WorkwoodDir is the per-repo folder holding child scripts.
const WorkwoodDir = ".workwood"

// Modes a plugin can run in.
const (
	ModeRun  = "run"
	ModeInit = "init"
)

// row is one repo's resolved entry in the context file.
type row struct {
	repo        string
	target      string // main | worktree | <custom>  (ignore is filtered out)
	dir         string // abs source dir
	branch      string // branch when target is a worktree
	workwoodDir string // <dir>/.workwood
}

// dirs returns the plugin search dirs. Plugins live only in the super-repo's
// committed workwood/plugins folder — there is no global plugin dir.
func dirs(cfg *config.Config) []string {
	return []string{cfg.PluginsDir}
}

// Find returns the path of an executable plugin named name, searching the
// project dir before the global dir. Returns "" if not found.
func Find(cfg *config.Config, name string) string {
	for _, d := range dirs(cfg) {
		p := filepath.Join(d, name)
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() && fi.Mode()&0o111 != 0 {
			return p
		}
	}
	return ""
}

// List returns the names of executable plugins available to a project, merging
// the project and global dirs (project shadows global), sorted.
func List(cfg *config.Config) []string {
	seen := map[string]bool{}
	for _, d := range dirs(cfg) {
		entries, err := os.ReadDir(d)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			info, err := e.Info()
			if err != nil || info.Mode()&0o111 == 0 {
				continue
			}
			seen[e.Name()] = true
		}
	}
	out := make([]string, 0, len(seen))
	for n := range seen {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// Run resolves the feature's repos to targets, writes the context file, and execs
// the named plugin in the given mode (run|init) with the context in its
// environment (stdio inherited so a tmux plugin can take over the terminal).
func Run(cfg *config.Config, m *manifest.Manifest, featTargets map[string][]targets.Target, name, mode string) error {
	if mode == "" {
		mode = ModeRun
	}
	path := Find(cfg, name)
	if path == "" {
		avail := List(cfg)
		if len(avail) == 0 {
			return i18n.Err("err.no_plugin_none", name, cfg.PluginsDir)
		}
		return i18n.Err("err.no_plugin_avail", name, strings.Join(avail, ", "))
	}

	rows := resolveRows(cfg, m, featTargets)

	featureDir := cfg.FeatureDir(m.Feature)
	if err := os.MkdirAll(featureDir, 0o755); err != nil {
		return err
	}
	ctxPath := filepath.Join(featureDir, "context.tsv")
	// Columns: repo  target  dir  workwoodDir  branch. branch is the only field
	// that can be empty, so it goes LAST — `IFS=$'\t' read` collapses consecutive
	// tabs, which would corrupt a mid-row empty field.
	var b strings.Builder
	for _, r := range rows {
		fmt.Fprintf(&b, "%s\t%s\t%s\t%s\t%s\n", r.repo, r.target, r.dir, r.workwoodDir, r.branch)
	}
	if err := os.WriteFile(ctxPath, []byte(b.String()), 0o644); err != nil {
		return err
	}

	cmd := exec.Command(path)
	cmd.Dir = cfg.Root
	cmd.Env = append(os.Environ(),
		"WORKWOOD_PROJECT="+cfg.ProjectSlug,
		"WORKWOOD_FEATURE="+m.Feature,
		"WORKWOOD_PLUGIN="+name,
		"WORKWOOD_MODE="+mode,
		"WORKWOOD_SESSION="+m.Feature,
		"WORKWOOD_CONTEXT="+ctxPath,
		// The active UI language, so a plugin can localize its own output if it
		// wants. workwood doesn't translate plugin text itself.
		"WORKWOOD_LANG="+i18n.Lang(),
	)
	// Team-shared, free-form manifest vars are exported as WORKWOOD_VAR_<KEY> so a
	// plugin can read a namespace etc. without workwood interpreting it.
	for k, v := range m.Vars {
		cmd.Env = append(cmd.Env, "WORKWOOD_VAR_"+envKey(k)+"="+v)
	}
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// envKey upper-cases a var key and replaces non-alphanumerics with '_' so it's a
// valid environment-variable name fragment.
func envKey(k string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(k) {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	return b.String()
}

// resolveRows builds one context row per distinct repo in the feature, in
// manifest order, applying each repo's target (or the automatic default). Repos
// targeted "ignore" are dropped entirely. Any non-built-in target string is
// handed through verbatim with the base clone as its dir.
func resolveRows(cfg *config.Config, m *manifest.Manifest, featTargets map[string][]targets.Target) []row {
	// Per repo: primary worktree (canonical <feature>/<repo> preferred), and a
	// lookup of every worktree by manifest path so an explicit target can pick one.
	type wt struct{ dir, branch string }
	primary := map[string]wt{}
	byPath := map[string]map[string]wt{}
	var order []string
	seen := map[string]bool{}
	for _, w := range m.Worktrees {
		if !seen[w.Repo] {
			seen[w.Repo] = true
			order = append(order, w.Repo)
		}
		entry := wt{dir: cfg.Abs(w.Path), branch: w.Branch}
		if _, ok := primary[w.Repo]; !ok {
			primary[w.Repo] = entry
		}
		if byPath[w.Repo] == nil {
			byPath[w.Repo] = map[string]wt{}
		}
		byPath[w.Repo][w.Path] = entry
	}
	for _, w := range m.Worktrees {
		if w.Path == m.Feature+"/"+w.Repo {
			primary[w.Repo] = wt{dir: cfg.Abs(w.Path), branch: w.Branch}
		}
	}

	mkRow := func(repo, target, dir, branch string) row {
		return row{repo: repo, target: target, dir: dir, branch: branch, workwoodDir: filepath.Join(dir, WorkwoodDir)}
	}

	var rows []row
	for _, repo := range order {
		base := cfg.BaseRepo(repo)
		primaryE := primary[repo]
		fb := primaryE.branch
		list := featTargets[repo]

		// No setups → the automatic default: worktree if checked out, else main.
		if len(list) == 0 {
			if primaryE.dir != "" && pathExists(primaryE.dir) {
				rows = append(rows, mkRow(repo, "worktree", primaryE.dir, primaryE.branch))
			} else {
				rows = append(rows, mkRow(repo, "main", base, fb))
			}
			continue
		}

		// ignore only matters as the SOLE state — drop ignore entries, and skip
		// the repo entirely when nothing else remains.
		active := make([]targets.Target, 0, len(list))
		for _, t := range list {
			if t.Source == targets.SourceIgnore {
				continue
			}
			active = append(active, t)
		}
		if len(active) == 0 {
			continue // the repo was [ignore] only → excluded
		}

		// One context row per remaining setup (additive).
		for _, t := range active {
			switch t.Source {
			case targets.SourceMain:
				rows = append(rows, mkRow(repo, "main", base, fb))
			case targets.SourceWorktree:
				e := primaryE
				if t.Worktree != "" {
					if pe, found := byPath[repo][t.Worktree]; found {
						e = pe
					}
				}
				rows = append(rows, mkRow(repo, "worktree", e.dir, e.branch))
			default:
				// Arbitrary, user-defined target (e.g. "deploy"): hand the raw
				// string + the base clone dir through so the repo's .workwood child
				// can interpret it however it likes.
				rows = append(rows, mkRow(repo, string(t.Source), base, fb))
			}
		}
	}
	return rows
}

func pathExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
