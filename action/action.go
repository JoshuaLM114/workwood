// Package action runs an executable action script against a feature's selected
// targets.
//
// An action is just a script (any language) in the super-repo's committed
// workwood/actions/ folder. workwood is deliberately dumb about meaning: it
// resolves the feature's enabled targets to a key→absolute-path map, writes that
// as context.yml, and execs the action with the file's path (WORKWOOD_TARGETS)
// plus the active super-feature slug (WORKWOOD_FEATURE) in its environment. The
// action decides what the paths mean (local, deployed, whatever).
package action

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/JoshuaLM114/workwood/config"
	"github.com/JoshuaLM114/workwood/i18n"
	"gopkg.in/yaml.v3"
)

// Find returns the path of an executable action named name in the project's
// actions dir, or "" if not found.
func Find(cfg *config.Config, name string) string {
	p := filepath.Join(cfg.ActionsDir, name)
	if fi, err := os.Stat(p); err == nil && !fi.IsDir() && fi.Mode()&0o111 != 0 {
		return p
	}
	return ""
}

// List returns the names of executable actions available to a project, sorted.
func List(cfg *config.Config) []string {
	entries, err := os.ReadDir(cfg.ActionsDir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil || info.Mode()&0o111 == 0 {
			continue
		}
		out = append(out, e.Name())
	}
	sort.Strings(out)
	return out
}

// Command writes the resolved targets (key→abs path) to <featureDir>/context.yml
// and returns a prepared *exec.Cmd for the named action — without running it. The
// caller runs it (Run for the CLI; tea.ExecProcess for the TUI, which must release
// the terminal so a tmux action can take over). set may be empty (the action
// decides); vars are the manifest's free-form vars, exported as WORKWOOD_VAR_<KEY>.
func Command(cfg *config.Config, slug, name string, set, vars map[string]string) (*exec.Cmd, error) {
	path := Find(cfg, name)
	if path == "" {
		avail := List(cfg)
		if len(avail) == 0 {
			return nil, i18n.Err("err.no_action_none", name, cfg.ActionsDir)
		}
		return nil, i18n.Err("err.no_action_avail", name, strings.Join(avail, ", "))
	}

	featureDir := cfg.FeatureDir(slug)
	if err := os.MkdirAll(featureDir, 0o755); err != nil {
		return nil, err
	}
	ctxPath := filepath.Join(featureDir, "context.yml")
	if err := writeContext(ctxPath, set); err != nil {
		return nil, err
	}

	cmd := exec.Command(path)
	cmd.Dir = cfg.Root
	cmd.Env = append(os.Environ(),
		"WORKWOOD_TARGETS="+ctxPath,
		"WORKWOOD_FEATURE="+slug,
		"WORKWOOD_ACTION="+name,
		"WORKWOOD_LANG="+i18n.Lang(),
	)
	for k, v := range vars {
		cmd.Env = append(cmd.Env, "WORKWOOD_VAR_"+envKey(k)+"="+v)
	}
	return cmd, nil
}

// Run prepares and runs an action with inherited stdio (the CLI path).
func Run(cfg *config.Config, slug, name string, set, vars map[string]string) error {
	cmd, err := Command(cfg, slug, name, set, vars)
	if err != nil {
		return err
	}
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// writeContext writes a key→path map as YAML. yaml.v3 sorts map keys, so the file
// is stable. A nil/empty set yields an empty map document.
func writeContext(path string, set map[string]string) error {
	if set == nil {
		set = map[string]string{}
	}
	data, err := yaml.Marshal(set)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
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
