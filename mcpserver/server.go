// Package mcpserver exposes workwood's project operations as structured MCP tools.
package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/JoshuaLM114/workwood/config"
	"github.com/JoshuaLM114/workwood/manifest"
	"github.com/JoshuaLM114/workwood/models"
	"github.com/JoshuaLM114/workwood/version"
)

// Options supplies an optional default project for calls that omit project.
type Options struct {
	Project string
	DataDir string
}

// Server owns the tool catalog and serializes operations on shared local state.
type Server struct {
	MCP     *server.MCPServer
	options Options
	gate    chan struct{}
}

type ProjectInput struct {
	Project string `json:"project,omitempty" jsonschema:"Registered project UUID or project/feature-folder path. Omit for the startup project or the only registered project."`
	DataDir string `json:"data_dir,omitempty" jsonschema:"Data directory for an unregistered project path; registered projects and feature links supply it automatically."`
}

type FeatureInput struct {
	ProjectInput
	Feature string `json:"feature,omitempty" jsonschema:"Super-feature slug. Omit only when the project path or server startup path is inside that feature."`
}

type result struct {
	Data  any    `json:"data,omitempty"`
	Error string `json:"error,omitempty"`
}

type effects struct{ readOnly, destructive, idempotent, openWorld bool }

var (
	readOnly = effects{readOnly: true, idempotent: true}
	write    = effects{}
	remove   = effects{destructive: true}
	network  = effects{openWorld: true}
	script   = effects{destructive: true, openWorld: true}
)

// New builds a server without requiring a project or writing any files.
func New(options Options) (*Server, error) {
	if options.Project == "" {
		loc, err := config.LocateProject("")
		var missing *config.NotInProjectError
		if err == nil || !errors.As(err, &missing) {
			// Preserve feature-folder context and surface invalid local projects
			// on tool calls instead of falling back to a different registration.
			options.Project, err = os.Getwd()
			if err != nil {
				return nil, err
			}
			if options.DataDir == "" && loc != nil {
				options.DataDir = loc.DataDir
			}
		}
	}
	s := &Server{options: options, gate: make(chan struct{}, 1)}
	s.MCP = server.NewMCPServer("workwood", version.Software,
		server.WithToolCapabilities(false), server.WithResourceCapabilities(false, false),
		server.WithInputSchemaValidation(), server.WithRecovery(),
		server.WithInstructions("Use projects_list to discover registered projects and the startup/cwd project. project_detect checks existing setup without writing files. When it reports needs_init, pass init_arguments to project_init; needs_data_dir requires the existing data path first. project_init upgrades metadata and registers the project while preserving checkouts and settings. Pass the chosen project UUID to subsequent tools. Pull source repos before creating worktrees. Use feature_get and targets_get to inspect paths; edit feature worktrees, never base clones. actions_list discovers project scripts without running them. action_execute runs a named script with explicit run, validate, or init mode. Folder-name and doctor repairs require explicit decisions from their inspection tools. Destructive tools can remove local changes. Tool results contain structured data and errors; inspect partial results on failure. Registry entries are local; project manifests and repo definitions are team-shared files. Workwood does not stage, commit, or push them."))
	s.addProjectTools()
	s.addRepoTools()
	s.addFeatureTools()
	s.addTargetTools()
	s.addActionTools()
	s.MCP.AddResource(mcp.NewResource("workwood://guide", "Workwood agent workflow", mcp.WithMIMEType("text/markdown")),
		func(context.Context, mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
			return []mcp.ResourceContents{mcp.TextResourceContents{URI: "workwood://guide", MIMEType: "text/markdown", Text: agentGuide}}, nil
		})
	return s, nil
}

// Serve runs the local JSON-RPC transport. Only protocol frames reach stdout.
func (s *Server) Serve(ctx context.Context, in io.Reader, out io.Writer) error {
	return server.NewStdioServer(s.MCP).Listen(ctx, in, out)
}

func addTool[T any](s *Server, name, description string, effect effects, handler func(context.Context, T) (any, error)) {
	tool := mcp.NewTool(name, mcp.WithDescription(description), mcp.WithInputSchema[T](), mcp.WithOutputSchema[result](),
		mcp.WithReadOnlyHintAnnotation(effect.readOnly), mcp.WithDestructiveHintAnnotation(effect.destructive),
		mcp.WithIdempotentHintAnnotation(effect.idempotent), mcp.WithOpenWorldHintAnnotation(effect.openWorld))
	s.MCP.AddTool(tool, mcp.NewTypedToolHandler(func(ctx context.Context, _ mcp.CallToolRequest, input T) (*mcp.CallToolResult, error) {
		select {
		case s.gate <- struct{}{}:
			defer func() { <-s.gate }()
		case <-ctx.Done():
			return mcp.NewToolResultError(ctx.Err().Error()), nil
		}
		if err := ctx.Err(); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		data, err := handler(ctx, input)
		body := result{Data: data}
		if err != nil {
			body.Error = err.Error()
		}
		res := mcp.NewToolResultStructuredOnly(body)
		res.IsError = err != nil
		return res, nil
	}))
}

func (s *Server) project(input ProjectInput) (*models.Config, *models.ProjectDef, error) {
	registered, err := config.ListProjects()
	if err != nil {
		return nil, nil, err
	}
	path, dataDir, expectedID := input.Project, input.DataDir, ""
	if path == "" {
		path = s.options.Project
		if dataDir == "" {
			dataDir = s.options.DataDir
		}
		if path == "" {
			if len(registered) != 1 {
				return nil, nil, fmt.Errorf("specify a project UUID or path; use projects_list to discover projects")
			}
			path = registered[0].ID
		}
	}
	for _, p := range registered {
		if path == p.ID {
			path, expectedID = p.Root, p.ID
			if dataDir == "" {
				dataDir = p.DataDir
			}
			break
		}
	}
	loc, err := config.LocateProject(path)
	if err != nil {
		return nil, nil, err
	}
	if expectedID != "" && expectedID != loc.PD.ID {
		return nil, nil, fmt.Errorf("registered project %s has a different identity at %s; register its current location", expectedID, path)
	}
	if loc.DataDir != "" {
		dataDir = loc.DataDir
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
		dataDir = os.Getenv(config.EnvData)
	}
	if dataDir == "" {
		app, _, err := config.LoadApp()
		if err != nil {
			return nil, nil, err
		}
		dataDir = app.DataDir
	}
	if dataDir == "" {
		return nil, nil, fmt.Errorf("data_dir is required; register the project with project_register or set WORKWOOD_DATA")
	}
	dataDir, err = filepath.Abs(dataDir)
	if err != nil {
		return nil, nil, err
	}
	cfg, err := config.Build(loc.Root, loc.PD, dataDir)
	if err != nil {
		return nil, nil, err
	}
	cfg.ActiveFeature = loc.ActiveFeature
	for _, repo := range loc.PD.Repos {
		if err := component(repo.Name); err != nil {
			return nil, nil, fmt.Errorf("invalid repo in workwood.yml: %w", err)
		}
	}
	return cfg, loc.PD, nil
}

func (s *Server) feature(input FeatureInput) (*models.Config, *models.ProjectDef, *models.Manifest, error) {
	cfg, pd, err := s.project(input.ProjectInput)
	if err != nil {
		return nil, nil, nil, err
	}
	name := input.Feature
	if name == "" {
		name = cfg.ActiveFeature
	}
	if err := component(name); err != nil {
		return nil, nil, nil, fmt.Errorf("feature: %w", err)
	}
	m, err := manifest.Load(cfg.ManifestPath(name))
	if err != nil {
		return nil, nil, nil, err
	}
	if m.ID == "" {
		return nil, nil, nil, fmt.Errorf("feature %q has no UUID; run project_init to adopt its manifest", name)
	}
	if m.Feature != name || (m.Project != "" && m.Project != cfg.ProjectID) {
		return nil, nil, nil, fmt.Errorf("manifest identity does not match project and feature %q", name)
	}
	if err := safeFeaturePaths(cfg, name, m.Worktrees); err != nil {
		return nil, nil, nil, err
	}
	return cfg, pd, m, nil
}

func safeFeaturePaths(cfg *models.Config, name string, worktrees []models.Worktree) error {
	paths := []string{cfg.FeatureDir(name), filepath.Join(cfg.FeatureDir(name), models.RepoWorkwoodDirName)}
	for _, w := range worktrees {
		if err := component(w.Repo); err != nil {
			return err
		}
		if filepath.ToSlash(filepath.Dir(w.Path)) != name || filepath.ToSlash(filepath.Clean(w.Path)) != w.Path || filepath.Base(w.Path) == models.RepoWorkwoodDirName {
			return fmt.Errorf("worktree path %q is not directly inside feature %q", w.Path, name)
		}
		paths = append(paths, cfg.Abs(w.Path))
	}
	for _, path := range paths {
		if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("feature path %q is a symlink", path)
		} else if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func component(name string) error {
	if strings.TrimSpace(name) == "" || name == "." || name == ".." || strings.HasPrefix(name, "-") || strings.ContainsAny(name, "/\\\x00\r\n") {
		return fmt.Errorf("%q must be a non-empty name without path separators", name)
	}
	return nil
}

const agentGuide = `# Workwood agent workflow

1. Use projects_list to discover registrations and the startup/cwd project. Use project_detect on an existing project to check setup readiness. For needs_init, pass its init_arguments to project_init; for needs_data_dir, supply the existing data path first. project_init upgrades metadata and registers the project without rebuilding checkouts. A new project needs an explicit path. Pass the selected UUID on subsequent tools.
2. Use repos_list and repos_pull to clone or fast-forward source repos. Inspect errors before creating branches. Base clones are reference scaffolding; edit feature worktrees.
3. Use feature_create, then feature_add_worktree for each repo/branch. One feature can contain several branches of one repo. New branch names must not exist and creation requires a successful origin fetch. Set base_source to origin for the fetched remote tip, local for the unchanged local tip, or pull to fast-forward origin into local before use. existing_branch explicitly attaches an existing branch.
4. Use feature_get for absolute paths and status. New folders use repo--branch with the feature prefix omitted. feature_up reconstructs missing checkouts.
5. Resolve old folder names with feature_folders_check and feature_folders_apply. Renaming preserves checkout changes; dropping keeps the checkout and branch. No choice is implied by a missing argument.
6. Use targets_get, targets_set and presets to choose the paths an action receives. actions_list only inspects scripts. action_execute explicitly invokes validate, init or run and captures output. Scripts have no interactive terminal over MCP.
7. feature_diagnose and feature_reconcile repair manifest/disk drift with explicit per-path decisions. Destructive removal, teardown and deletion can discard local changes; inspect feature_get first.

Project definitions, manifests and action scripts are shared files. The registry, clones, worktrees, presets and working sets are local. Tools leave shared edits unstaged; committing and pushing are separate operations.
`
