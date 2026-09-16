package mcpserver

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/JoshuaLM114/workwood/config"
	"github.com/JoshuaLM114/workwood/i18n"
	"github.com/JoshuaLM114/workwood/version"
)

type registerInput struct {
	Path    string `json:"path" jsonschema:"Super-repo path or a feature folder containing a workwood back-link."`
	DataDir string `json:"data_dir,omitempty" jsonschema:"Project data directory. Required unless a feature link supplies it."`
}

type initInput struct {
	Path    string `json:"path,omitempty" jsonschema:"Existing project path or registered UUID, or an explicit directory for a new project. Omit to upgrade the detected startup/cwd project."`
	DataDir string `json:"data_dir,omitempty" jsonschema:"Existing local clone/worktree/state directory. Omit to reuse the feature link, registration, environment or saved fallback."`
	Name    string `json:"name,omitempty" jsonschema:"Shared name for a new project. Existing project names are preserved."`
}

type renameProjectInput struct {
	ProjectInput
	Name string `json:"name" jsonschema:"Local display name; the committed project identity remains unchanged."`
}

type settingsInput struct {
	Language    *string `json:"language,omitempty" jsonschema:"UI language: en or ja."`
	UpdateCheck *bool   `json:"update_check,omitempty" jsonschema:"Enable the CLI's update notice; MCP startup never checks for updates."`
	DataDir     *string `json:"data_dir,omitempty" jsonschema:"Global fallback data directory; empty clears it. Registered projects keep their own data directories."`
}

func (s *Server) addProjectTools() {
	addTool(s, "workwood_version", "Get the workwood software and file-schema versions.", readOnly, func(_ context.Context, _ struct{}) (any, error) {
		return map[string]any{"software": version.Software, "schema": version.Schema, "setup": config.SetupVersion}, nil
	})
	addTool(s, "projects_list", "Discover registered projects and the startup/cwd project, including unregistered older setups. Report setup readiness and stale registrations without writing files.", readOnly, func(_ context.Context, _ struct{}) (any, error) {
		projects, err := config.ListProjects()
		if err != nil {
			return nil, err
		}
		rows := []map[string]any{}
		for _, p := range projects {
			row := map[string]any{"id": p.ID, "name": p.Name, "root": p.Root, "data_dir": p.DataDir, "available": true}
			if cfg, _, err := s.project(ProjectInput{Project: p.ID}); err != nil {
				row["available"], row["error"] = false, err.Error()
			} else {
				row["name"] = cfg.ProjectName
			}
			row["setup"], _ = config.DetectProject(p.ID, "")
			rows = append(rows, row)
		}
		detected, _ := config.DetectProject(s.options.Project, s.options.DataDir)
		return map[string]any{"projects": rows, "startup_project": s.options.Project, "detected_project": detected}, nil
	})
	addTool(s, "project_detect", "Find an existing project and check whether its local setup and registration are current. Returns ready, needs_init, needs_data_dir, not_found, or blocked, with issues and project_init arguments. Read-only; does not scan the disk or pull repos.", readOnly, func(_ context.Context, in struct {
		Project string `json:"project,omitempty" jsonschema:"Registered UUID or path to inspect, including a nested project directory or linked feature folder. Omit for the startup project or cwd."`
		DataDir string `json:"data_dir,omitempty" jsonschema:"Existing project data directory if it cannot be discovered automatically."`
	}) (any, error) {
		if in.Project == "" {
			in.Project = s.options.Project
			if in.DataDir == "" {
				in.DataDir = s.options.DataDir
			}
		}
		return config.DetectProject(in.Project, in.DataDir)
	})
	addTool(s, "project_register", "Register an existing project's path and data directory for MCP discovery. Does not clone repos or modify project files.", write, func(_ context.Context, in registerInput) (any, error) {
		if strings.TrimSpace(in.Path) == "" {
			return nil, fmt.Errorf("path is required")
		}
		return config.RegisterProject(in.Path, in.DataDir)
	})
	addTool(s, "project_unregister", "Remove only a project registry entry. The super-repo, data, checkouts and branches remain intact.", remove, func(_ context.Context, in struct {
		ID string `json:"id" jsonschema:"Registered project UUID from projects_list."`
	}) (any, error) {
		return nil, config.UnregisterProject(in.ID)
	})
	addTool(s, "project_init", "Initialize or upgrade detected project metadata and register the project. Reuses known data paths, fills missing identities and back-links, and records the setup version. Preserves worktrees, branches and local settings. Use project_detect first; a new project needs an explicit path. Does not initialize Git or clone repos.", write, func(_ context.Context, in initInput) (any, error) {
		if in.Path == "" {
			in.Path = s.options.Project
			if in.DataDir == "" {
				in.DataDir = s.options.DataDir
			}
		}
		return config.InitializeProject(in.Path, in.DataDir, in.Name)
	})
	addTool(s, "project_info", "Get the resolved project's identity, local paths and repo definition.", readOnly, func(_ context.Context, in ProjectInput) (any, error) {
		cfg, pd, err := s.project(in)
		if err != nil {
			return nil, err
		}
		return map[string]any{"id": cfg.ProjectID, "name": cfg.ProjectName, "root": cfg.Root, "data_dir": cfg.DataDir, "main_dir": cfg.MainDir, "features_dir": cfg.FeaturesDir, "actions_dir": cfg.ActionsDir, "active_feature": cfg.ActiveFeature, "definition": pd}, nil
	})
	addTool(s, "project_rename", "Change a project's local display name.", write, func(_ context.Context, in renameProjectInput) (any, error) {
		cfg, _, err := s.project(in.ProjectInput)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(in.Name) == "" {
			return nil, fmt.Errorf("name is required")
		}
		st, err := config.LoadState(cfg.StateFile)
		if err != nil {
			return nil, err
		}
		st.Project, st.Name = cfg.ProjectID, in.Name
		return nil, config.SaveState(cfg.StateFile, st)
	})
	addTool(s, "settings_get", "Read global workwood settings and the current UI language.", readOnly, func(_ context.Context, _ struct{}) (any, error) {
		app, home, err := config.LoadApp()
		return map[string]any{"settings": app, "home": home, "language": i18n.Lang()}, err
	})
	addTool(s, "settings_update", "Update selected global settings: language, update notices and fallback data directory.", write, func(_ context.Context, in settingsInput) (any, error) {
		app, home, err := config.LoadApp()
		if err != nil {
			return nil, err
		}
		if in.Language != nil {
			if !i18n.IsSupported(*in.Language) {
				return nil, fmt.Errorf("language must be en or ja")
			}
			app.Language = *in.Language
		}
		if in.UpdateCheck != nil {
			app.UpdateCheck = in.UpdateCheck
		}
		if in.DataDir != nil {
			app.DataDir = *in.DataDir
			if app.DataDir != "" {
				app.DataDir, err = filepath.Abs(app.DataDir)
				if err != nil {
					return nil, err
				}
			}
		}
		if err := config.SaveApp(home, app); err != nil {
			return nil, err
		}
		if in.Language != nil {
			i18n.Init(*in.Language)
		}
		return app, nil
	})
}
