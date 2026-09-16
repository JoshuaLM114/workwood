package mcpserver

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/JoshuaLM114/workwood/action"
	"github.com/JoshuaLM114/workwood/config"
	"github.com/JoshuaLM114/workwood/models"
	"github.com/JoshuaLM114/workwood/targetcfg"
)

type executeActionInput struct {
	FeatureInput
	Action         string      `json:"action" jsonschema:"Exact script filename from actions_list."`
	Mode           string      `json:"mode" jsonschema:"Lifecycle function to invoke: run, validate or init. Run does not implicitly invoke Validate."`
	Targets        *models.Set `json:"targets,omitempty" jsonschema:"Optional target override for this invocation; does not change the working set."`
	Preset         string      `json:"preset,omitempty" jsonschema:"Optional named target preset; mutually exclusive with targets."`
	Input          string      `json:"input,omitempty" jsonschema:"Optional text supplied to the script's stdin. No terminal or interactive prompts are available."`
	TimeoutSeconds int         `json:"timeout_seconds,omitempty" jsonschema:"Execution timeout in seconds, from 1 to 3600. Defaults to 300."`
}

// limitedOutput keeps draining both command streams after reaching the limit.
// os/exec serializes writes when stdout and stderr share this writer.
type limitedOutput struct {
	buffer    bytes.Buffer
	truncated bool
}

func (b *limitedOutput) Write(p []byte) (int, error) {
	n := len(p)
	remaining := (1 << 20) - b.buffer.Len()
	if len(p) > remaining {
		p = p[:remaining]
		b.truncated = true
	}
	_, _ = b.buffer.Write(p)
	return n, nil
}

func (s *Server) addActionTools() {
	addTool(s, "actions_list", "Discover executable action scripts, declared lifecycle functions and marked scripts that need executable permission. Does not source scripts or run Validate.", readOnly, func(_ context.Context, in ProjectInput) (any, error) {
		cfg, _, err := s.project(in)
		if err != nil {
			return nil, err
		}
		if _, err := os.ReadDir(cfg.ActionsDir); err != nil {
			return nil, err
		}
		actions, needChmod := action.Scan(cfg)
		return map[string]any{"actions": actions, "need_chmod": needChmod, "directory": cfg.ActionsDir}, nil
	})
	addTool(s, "action_enable", "Make a marked action script executable so workwood can discover it. Does not run the script.", write, func(_ context.Context, in struct {
		ProjectInput
		Action string `json:"action"`
	}) (any, error) {
		cfg, _, err := s.project(in.ProjectInput)
		if err != nil {
			return nil, err
		}
		if err := component(in.Action); err != nil {
			return nil, err
		}
		_, needChmod := action.Scan(cfg)
		if !slices.Contains(needChmod, in.Action) {
			return nil, fmt.Errorf("action must be one of actions_list.need_chmod")
		}
		path := filepath.Join(cfg.ActionsDir, in.Action)
		info, err := os.Lstat(path)
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("action must be a regular file")
		}
		return nil, os.Chmod(path, info.Mode().Perm()|0o111)
	})
	addTool(s, "action_execute", "Invoke a project's Run, Validate or Init action function with selected targets and manifest variables. Scripts can modify files or external systems in any mode. Captures combined output (up to 1 MiB), exit status and truncation. Supports cancellation and a bounded timeout; no terminal is available.", script, func(ctx context.Context, in executeActionInput) (any, error) {
		cfg, pd, m, err := s.feature(in.FeatureInput)
		if err != nil {
			return nil, err
		}
		if err := component(in.Action); err != nil {
			return nil, err
		}
		if in.Mode != "run" && in.Mode != "validate" && in.Mode != "init" {
			return nil, fmt.Errorf("mode must be run, validate or init")
		}
		if !slices.Contains(action.Names(cfg), in.Action) {
			return nil, fmt.Errorf("unknown executable action %q; use actions_list", in.Action)
		}
		if in.Targets != nil && in.Preset != "" {
			return nil, fmt.Errorf("specify targets or preset, not both")
		}
		if in.TimeoutSeconds == 0 {
			in.TimeoutSeconds = 300
		}
		if in.TimeoutSeconds < 1 || in.TimeoutSeconds > 3600 {
			return nil, fmt.Errorf("timeout_seconds must be between 1 and 3600")
		}
		var set models.Set
		switch {
		case in.Targets != nil:
			set = *in.Targets
		case in.Preset != "":
			if err := component(in.Preset); err != nil {
				return nil, err
			}
			set, err = targetcfg.LoadPreset(cfg, in.Preset)
		default:
			set, err = targetcfg.Working(cfg, pd, m)
		}
		if err != nil {
			return nil, err
		}
		if err := validateTargets(set); err != nil {
			return nil, err
		}
		if _, err := config.EnsureFeatureLink(cfg, m.Feature); err != nil {
			return nil, err
		}
		ctx, cancel := context.WithTimeout(ctx, time.Duration(in.TimeoutSeconds)*time.Second)
		defer cancel()
		cmd, err := action.CommandContext(ctx, cfg, m.Feature, in.Action, in.Mode, set, m.Vars)
		if err != nil {
			return nil, err
		}
		output := &limitedOutput{}
		cmd.Stdout, cmd.Stderr = output, output
		cmd.Stdin = strings.NewReader(in.Input)
		started := time.Now()
		err = cmd.Run()
		exitCode := 0
		if err != nil {
			exitCode = -1
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) {
				exitCode = exitErr.ExitCode()
			}
		}
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		return map[string]any{"action": in.Action, "mode": in.Mode, "output": output.buffer.String(), "truncated": output.truncated, "exit_code": exitCode, "duration_ms": time.Since(started).Milliseconds(), "targets": set}, err
	})
}
