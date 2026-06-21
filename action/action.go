// Package action runs an executable action script against a feature's selected
// targets.
//
// An action is a script (any language) in the super-repo's committed
// workwood/actions/ folder that OPTS IN with a marker comment in its first lines:
//
//	# workwood-action: deploy to dev      (the text after ':' is an optional label)
//
// That marker is how workwood tells an action from a stray helper executable —
// there's no way to validate a shell script's argument signature, so an explicit
// opt-in is the safe substitute (it's grepped, never executed).
//
// workwood is deliberately dumb about meaning: it resolves the feature's enabled
// targets to a key→absolute-path map, writes that as context.yml, and execs the
// action with the file's path BOTH as $1 and as WORKWOOD_TARGETS, plus the active
// super-feature slug (WORKWOOD_FEATURE). The action decides what the paths mean.
package action

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/JoshuaLM114/workwood/config"
	"github.com/JoshuaLM114/workwood/i18n"
	"gopkg.in/yaml.v3"
)

// Action is a discovered action: its filename + the optional marker label.
type Action struct {
	Name        string
	Description string
}

// markerRe matches the opt-in marker line, capturing an optional description.
// The \b stops it from matching "workwood-actions" or similar.
var markerRe = regexp.MustCompile(`^\s*#\s*workwood-action\b\s*:?\s*(.*)$`)

// meta reads a candidate file's first lines and, if it carries the
// `# workwood-action` marker, returns its description (may be "") and ok=true.
func meta(path string) (desc string, ok bool) {
	f, err := os.Open(path)
	if err != nil {
		return "", false
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for i := 0; i < 30 && sc.Scan(); i++ {
		if m := markerRe.FindStringSubmatch(sc.Text()); m != nil {
			return strings.TrimSpace(m[1]), true
		}
	}
	return "", false
}

// Find returns the path of a valid action named name (executable + marker), or "".
func Find(cfg *config.Config, name string) string {
	p := filepath.Join(cfg.ActionsDir, name)
	if fi, err := os.Stat(p); err == nil && !fi.IsDir() && fi.Mode()&0o111 != 0 {
		if _, ok := meta(p); ok {
			return p
		}
	}
	return ""
}

// Scan inspects the actions dir once and returns the valid actions (marked +
// executable) plus the names of files that carry the marker but AREN'T executable
// — a common mistake worth reporting, since workwood execs actions directly.
func Scan(cfg *config.Config) (actions []Action, needChmod []string) {
	entries, err := os.ReadDir(cfg.ActionsDir)
	if err != nil {
		return nil, nil
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		desc, ok := meta(filepath.Join(cfg.ActionsDir, e.Name()))
		if !ok {
			continue // not marked → not an action, ignore silently
		}
		if info.Mode()&0o111 == 0 {
			needChmod = append(needChmod, e.Name())
			continue
		}
		actions = append(actions, Action{Name: e.Name(), Description: desc})
	}
	sort.Slice(actions, func(i, j int) bool { return actions[i].Name < actions[j].Name })
	sort.Strings(needChmod)
	return actions, needChmod
}

// List returns the marked, executable actions available to a project, sorted.
func List(cfg *config.Config) []Action {
	actions, _ := Scan(cfg)
	return actions
}

// Names returns just the action names (for messages).
func Names(cfg *config.Config) []string {
	as := List(cfg)
	names := make([]string, len(as))
	for i, a := range as {
		names[i] = a.Name
	}
	return names
}

// Command writes the resolved targets (key→abs path) to <featureDir>/context.yml
// and returns a prepared *exec.Cmd for the named action — without running it. The
// caller runs it (Run for the CLI; tea.ExecProcess for the TUI, which must release
// the terminal so a tmux action can take over). set may be empty (the action
// decides); vars are the manifest's free-form vars, exported as WORKWOOD_VAR_<KEY>.
func Command(cfg *config.Config, slug, name string, set, vars map[string]string) (*exec.Cmd, error) {
	path := Find(cfg, name)
	if path == "" {
		avail := Names(cfg)
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

	// The targets file is passed BOTH as $1 (so an action is literally "a command
	// that takes the targets as an argument") and as WORKWOOD_TARGETS.
	cmd := exec.Command(path, ctxPath)
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
