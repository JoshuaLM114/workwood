package mcpserver

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/JoshuaLM114/workwood/config"
	"github.com/JoshuaLM114/workwood/models"
	"github.com/JoshuaLM114/workwood/targetcfg"
)

type presetInput struct {
	ProjectInput
	Name string `json:"name" jsonschema:"Literal preset name from presets_list, without an extension or directory."`
}

func validateTargets(set models.Set) error {
	for key, path := range set {
		if strings.TrimSpace(key) == "" || strings.ContainsAny(key, "\x00\r\n") {
			return fmt.Errorf("target keys must be non-empty single-line names")
		}
		if !filepath.IsAbs(path) || strings.ContainsRune(path, '\x00') {
			return fmt.Errorf("target %q must have an absolute path", key)
		}
	}
	return nil
}

func (s *Server) addTargetTools() {
	addTool(s, "targets_get", "Inspect a feature's effective enabled targets and candidate tree, including monorepo services and additional paths.", readOnly, func(_ context.Context, in FeatureInput) (any, error) {
		cfg, pd, m, err := s.feature(in)
		if err != nil {
			return nil, err
		}
		set, err := targetcfg.Working(cfg, pd, m)
		if err != nil {
			return nil, err
		}
		return map[string]any{"targets": set, "candidates": targetcfg.Candidates(cfg, pd, m, set)}, nil
	})
	addTool(s, "targets_set", "Replace a feature's enabled target map. Supports adding/removing paths and renaming keys. Absolute external paths are allowed; an empty object disables every target.", write, func(_ context.Context, in struct {
		FeatureInput
		Targets models.Set `json:"targets"`
	}) (any, error) {
		cfg, _, m, err := s.feature(in.FeatureInput)
		if err != nil {
			return nil, err
		}
		if err := validateTargets(in.Targets); err != nil {
			return nil, err
		}
		return in.Targets, targetcfg.SaveWorking(cfg, m, in.Targets)
	})
	addTool(s, "targets_generate", "Generate a clean preset from the feature's configured base repos and worktrees. Defaults to a preset named after the feature; leaves the active working set unchanged.", write, func(_ context.Context, in struct {
		FeatureInput
		Name      string `json:"name,omitempty"`
		Overwrite bool   `json:"overwrite,omitempty"`
	}) (any, error) {
		cfg, pd, m, err := s.feature(in.FeatureInput)
		if err != nil {
			return nil, err
		}
		if in.Name == "" {
			in.Name = m.Feature
		}
		if err := component(in.Name); err != nil {
			return nil, err
		}
		if !in.Overwrite && targetcfg.PresetExists(cfg, in.Name) {
			return nil, fmt.Errorf("preset %q exists; set overwrite to replace it", in.Name)
		}
		set := targetcfg.CleanSet(cfg, pd, m)
		return set, targetcfg.SavePreset(cfg, in.Name, set)
	})
	addTool(s, "targets_prune", "Remove missing managed checkout paths from the saved working set. Keeps external targets even if unavailable.", remove, func(_ context.Context, in FeatureInput) (any, error) {
		cfg, pd, m, err := s.feature(in)
		if err != nil {
			return nil, err
		}
		set, err := targetcfg.Working(cfg, pd, m)
		if err != nil {
			return nil, err
		}
		removed := targetcfg.Prune(cfg, set)
		return map[string]any{"removed": removed, "targets": set}, targetcfg.SaveWorking(cfg, m, set)
	})
	addTool(s, "targets_expand", "Read a directory's .workwood/targets.yml and resolve its monorepo service paths. Does not enable targets or run scripts.", readOnly, func(_ context.Context, in struct {
		Path string `json:"path"`
	}) (any, error) {
		if !filepath.IsAbs(in.Path) {
			return nil, fmt.Errorf("path must be absolute")
		}
		return targetcfg.ExpandServices(in.Path)
	})
	addTool(s, "presets_list", "List local target presets for a project.", readOnly, func(_ context.Context, in ProjectInput) (any, error) {
		cfg, _, err := s.project(in)
		if err != nil {
			return nil, err
		}
		return targetcfg.ListPresets(cfg), nil
	})
	addTool(s, "preset_get", "Read a named target preset without applying it.", readOnly, func(_ context.Context, in presetInput) (any, error) {
		cfg, _, err := s.project(in.ProjectInput)
		if err != nil {
			return nil, err
		}
		if err := component(in.Name); err != nil {
			return nil, err
		}
		return targetcfg.LoadPreset(cfg, in.Name)
	})
	addTool(s, "preset_save", "Save the current working set, or an explicit target map, as a local preset. Replacing an existing preset requires overwrite=true.", write, func(_ context.Context, in struct {
		FeatureInput
		Name      string      `json:"name"`
		Targets   *models.Set `json:"targets,omitempty" jsonschema:"Omit to save the current feature working set."`
		Overwrite bool        `json:"overwrite,omitempty"`
	}) (any, error) {
		cfg, pd, m, err := s.feature(in.FeatureInput)
		if err != nil {
			return nil, err
		}
		if err := component(in.Name); err != nil {
			return nil, err
		}
		if !in.Overwrite && targetcfg.PresetExists(cfg, in.Name) {
			return nil, fmt.Errorf("preset %q exists; set overwrite to replace it", in.Name)
		}
		var set models.Set
		if in.Targets != nil {
			set = *in.Targets
		} else {
			set, err = targetcfg.Working(cfg, pd, m)
			if err != nil {
				return nil, err
			}
		}
		if err := validateTargets(set); err != nil {
			return nil, err
		}
		if err := targetcfg.SavePreset(cfg, in.Name, set); err != nil {
			return nil, err
		}
		st, err := config.LoadState(cfg.StateFile)
		if err != nil {
			return set, err
		}
		st.EnsureFeature(m.ID, m.Feature)
		st.SetLastPreset(m.ID, in.Name)
		return set, config.SaveState(cfg.StateFile, st)
	})
	addTool(s, "preset_load", "Replace a feature's working set with a named preset and remember it as the last preset.", write, func(_ context.Context, in struct {
		FeatureInput
		Name string `json:"name"`
	}) (any, error) {
		cfg, _, m, err := s.feature(in.FeatureInput)
		if err != nil {
			return nil, err
		}
		if err := component(in.Name); err != nil {
			return nil, err
		}
		set, err := targetcfg.LoadPreset(cfg, in.Name)
		if err != nil {
			return nil, err
		}
		if err := validateTargets(set); err != nil {
			return nil, err
		}
		st, err := config.LoadState(cfg.StateFile)
		if err != nil {
			return nil, err
		}
		st.EnsureFeature(m.ID, m.Feature)
		st.SetWorkingSet(m.ID, set)
		st.SetLastPreset(m.ID, in.Name)
		return set, config.SaveState(cfg.StateFile, st)
	})
	addTool(s, "preset_delete", "Delete one local target preset. Existing working sets and checkouts remain intact.", remove, func(_ context.Context, in presetInput) (any, error) {
		cfg, _, err := s.project(in.ProjectInput)
		if err != nil {
			return nil, err
		}
		if err := component(in.Name); err != nil {
			return nil, err
		}
		return nil, os.Remove(targetcfg.PresetPath(cfg, in.Name))
	})
}
