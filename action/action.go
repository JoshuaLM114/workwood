// Package action runs an action script against a feature's selected targets.
//
// An action is a bash script in the super-repo's committed workwood/actions/
// folder that OPTS IN with a marker comment and defines two functions, Run and
// Validate:
//
//	# workwood-action: deploy to dev      (the text after ':' is an optional label)
//	Validate() { … return non-zero if the action can't act on the targets … }
//	Run()      { … do the thing … }
//
// workwood SOURCES the script and calls one function: Run when the user runs it,
// Validate as a pre-flight availability check. The marker tells an action from a
// stray helper; an action that lacks Run or Validate is reported as unavailable.
//
// workwood is deliberately dumb about meaning: it resolves the feature's enabled
// targets to a key→absolute-path map, writes that as context.yml, and exports the
// file's path as WORKWOOD_TARGETS (+ WORKWOOD_FEATURE/ACTION/LANG and the
// manifest's WORKWOOD_VAR_*). Run and Validate read those and decide what the
// paths mean.
package action

import (
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

// Action is a discovered action: its filename, the optional marker label, and
// whether it defines the required Run / Validate functions.
type Action struct {
	Name        string
	Description string
	HasRun      bool
	HasValidate bool
}

// Runnable reports whether an action defines both required functions.
func (a Action) Runnable() bool { return a.HasRun && a.HasValidate }

// markerRe matches the opt-in marker line, capturing an optional description.
// The \b stops it from matching "workwood-actions" or similar.
var markerRe = regexp.MustCompile(`^\s*#\s*workwood-action\b\s*:?\s*(.*)$`)

// runFuncRe / validateFuncRe match a bash function definition for Run / Validate,
// in either `Name() {` or `function Name {` form.
var (
	runFuncRe      = regexp.MustCompile(`(?m)^[ \t]*(function[ \t]+Run\b|Run[ \t]*\([ \t]*\))`)
	validateFuncRe = regexp.MustCompile(`(?m)^[ \t]*(function[ \t]+Validate\b|Validate[ \t]*\([ \t]*\))`)
)

// scanScript reads a candidate file once: its marker description (+ whether it's
// marked at all), and whether it defines Run / Validate.
func scanScript(path string) (desc string, marked, hasRun, hasValidate bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false, false, false
	}
	for _, line := range strings.Split(string(data), "\n") {
		if m := markerRe.FindStringSubmatch(line); m != nil {
			desc, marked = strings.TrimSpace(m[1]), true
			break
		}
	}
	return desc, marked, runFuncRe.Match(data), validateFuncRe.Match(data)
}

// Find returns the path of a discovered action named name (executable + marker),
// or "". It does NOT require Run/Validate — that's surfaced as availability.
func Find(cfg *config.Config, name string) string {
	p := filepath.Join(cfg.ActionsDir, name)
	if fi, err := os.Stat(p); err == nil && !fi.IsDir() && fi.Mode()&0o111 != 0 {
		if _, marked, _, _ := scanScript(p); marked {
			return p
		}
	}
	return ""
}

// Scan inspects the actions dir once and returns the discovered actions (marked +
// executable, each flagged for Run/Validate) plus the names of marked files that
// AREN'T executable — a common mistake worth reporting.
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
		desc, marked, hasRun, hasValidate := scanScript(filepath.Join(cfg.ActionsDir, e.Name()))
		if !marked {
			continue // not marked → not an action, ignore silently
		}
		if info.Mode()&0o111 == 0 {
			needChmod = append(needChmod, e.Name())
			continue
		}
		actions = append(actions, Action{Name: e.Name(), Description: desc, HasRun: hasRun, HasValidate: hasValidate})
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

// invoke writes the resolved targets to <featureDir>/context.yml and returns a
// prepared *exec.Cmd that SOURCES the action and calls one function (fn is "Run"
// or "Validate"). It errors if the action lacks that function. fn is an internal
// constant (never user input), so interpolating it into bash -c is safe.
func invoke(cfg *config.Config, slug, name, fn string, set, vars map[string]string) (*exec.Cmd, error) {
	path := Find(cfg, name)
	if path == "" {
		avail := Names(cfg)
		if len(avail) == 0 {
			return nil, i18n.Err("err.no_action_none", name, cfg.ActionsDir)
		}
		return nil, i18n.Err("err.no_action_avail", name, strings.Join(avail, ", "))
	}
	_, _, hasRun, hasValidate := scanScript(path)
	if (fn == "Run" && !hasRun) || (fn == "Validate" && !hasValidate) {
		return nil, i18n.Err("err.action_missing_method", name, fn)
	}

	featureDir := cfg.FeatureDir(slug)
	if err := os.MkdirAll(featureDir, 0o755); err != nil {
		return nil, err
	}
	ctxPath := filepath.Join(featureDir, "context.yml")
	if err := writeContext(ctxPath, set); err != nil {
		return nil, err
	}

	// A private state dir the action owns (workwood creates it but writes nothing
	// inside): the action persists/sources its own state there across runs.
	actionData := cfg.ActionDataDir(slug, name)
	if err := os.MkdirAll(actionData, 0o755); err != nil {
		return nil, err
	}

	// Source the action and call the requested function. The script's path is $1
	// to the -c program; the function reads the targets via WORKWOOD_TARGETS.
	cmd := exec.Command("bash", "-c", `source "$1"; `+fn, "workwood-action", path)
	cmd.Dir = cfg.Root
	cmd.Env = append(os.Environ(),
		"WORKWOOD_TARGETS="+ctxPath,
		"WORKWOOD_ACTION_DATA="+actionData,
		"WORKWOOD_FEATURE="+slug,
		"WORKWOOD_ACTION="+name,
		"WORKWOOD_LANG="+i18n.Lang(),
	)
	for k, v := range vars {
		cmd.Env = append(cmd.Env, "WORKWOOD_VAR_"+envKey(k)+"="+v)
	}
	return cmd, nil
}

// Command returns a prepared *exec.Cmd that runs the action's Run function,
// without executing it — the caller runs it (Run for the CLI; tea.ExecProcess for
// the TUI, which must release the terminal so a tmux action can take over). An
// action must define BOTH Run and Validate to be runnable.
func Command(cfg *config.Config, slug, name string, set, vars map[string]string) (*exec.Cmd, error) {
	if path := Find(cfg, name); path != "" {
		if _, _, _, hasValidate := scanScript(path); !hasValidate {
			return nil, i18n.Err("err.action_missing_method", name, "Validate")
		}
	}
	return invoke(cfg, slug, name, "Run", set, vars)
}

// Validate runs the action's Validate function against the targets as a pre-flight
// availability check, captured (no terminal hand-off). It returns nil when the
// action reports it can act on the targets (exit 0), else an error.
func Validate(cfg *config.Config, slug, name string, set, vars map[string]string) error {
	cmd, err := invoke(cfg, slug, name, "Validate", set, vars)
	if err != nil {
		return err
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return i18n.Errw(err, "err.action_validate_failed", name, strings.TrimSpace(string(out)))
	}
	return nil
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
