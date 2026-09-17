package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/JoshuaLM114/workwood/config"
	"github.com/JoshuaLM114/workwood/manifest"
	"github.com/JoshuaLM114/workwood/models"
	"github.com/JoshuaLM114/workwood/superfeature"
)

type createFeatureInput struct {
	ProjectInput
	Name        string `json:"name" jsonschema:"Unique feature slug, used in its manifest filename and data directory."`
	Shorthand   string `json:"shorthand,omitempty" jsonschema:"Branch prefix. Defaults to the feature name's initials."`
	Description string `json:"description,omitempty"`
}

type updateFeatureInput struct {
	FeatureInput
	DisplayName *string            `json:"display_name,omitempty" jsonschema:"Local display name; does not change the slug or branch prefix."`
	Description *string            `json:"description,omitempty"`
	Vars        *map[string]string `json:"vars,omitempty" jsonschema:"Replace the shared action variables; an empty object clears them."`
}

type addWorktreeInput struct {
	FeatureInput
	Repo            string `json:"repo"`
	Branch          string `json:"branch" jsonschema:"New branch suffix, or full branch name when existing_branch or no_feature_prefix is true."`
	From            string `json:"from,omitempty" jsonschema:"Starting ref for a new branch; defaults to the repo default branch."`
	BaseSource      string `json:"base_source,omitempty" jsonschema:"Starting-ref policy for a new branch: origin, local, or pull. Empty prefers origin when available, otherwise local. pull fast-forwards the local base from origin before use."`
	NoFeaturePrefix bool   `json:"no_feature_prefix,omitempty"`
	ExistingBranch  bool   `json:"existing_branch,omitempty" jsonschema:"Explicitly attach an existing local or origin branch instead of creating one."`
}

type removeWorktreeInput struct {
	FeatureInput
	Repo         string `json:"repo"`
	Branch       string `json:"branch" jsonschema:"Exact full branch from feature_get; required even when the repo has only one checkout."`
	DeleteBranch bool   `json:"delete_branch" jsonschema:"Also force-delete the local branch, including unmerged commits."`
}

type teardownChoice struct {
	Path           string `json:"path" jsonschema:"Exact features-relative path from feature_get."`
	RemoveWorktree bool   `json:"remove_worktree"`
	DeleteFiles    bool   `json:"delete_files"`
	DeleteBranch   bool   `json:"delete_branch"`
}

type repairChoice struct {
	Path   string `json:"path" jsonschema:"Exact features-relative path from feature_diagnose."`
	Action string `json:"action" jsonschema:"Orphans: adopt or remove. Missing checkouts: rebuild or drop. Omitted paths are left alone."`
}

func (s *Server) addFeatureTools() {
	addTool(s, "features_list", "List all committed super-feature manifests with local display names.", readOnly, func(_ context.Context, in ProjectInput) (any, error) {
		cfg, _, err := s.project(in)
		if err != nil {
			return nil, err
		}
		mans, err := superfeature.List(cfg)
		if err != nil {
			return nil, err
		}
		st, err := config.LoadState(cfg.StateFile)
		if err != nil {
			return nil, err
		}
		rows := []map[string]any{}
		for _, m := range mans {
			name := m.Feature
			if f, ok := st.Features[m.ID]; ok {
				name = f.DisplayName()
			}
			rows = append(rows, map[string]any{"manifest": m, "display_name": name})
		}
		return rows, nil
	})
	addTool(s, "feature_get", "Inspect a feature's manifest, absolute checkout paths, branch status and dirty-file counts before editing or removing it.", readOnly, func(_ context.Context, in FeatureInput) (any, error) {
		cfg, _, m, err := s.feature(in)
		if err != nil {
			return nil, err
		}
		_, rows, err := superfeature.Status(cfg, m.Feature)
		if err != nil {
			return nil, err
		}
		worktrees := []map[string]any{}
		for _, r := range rows {
			worktrees = append(worktrees, map[string]any{"repo": r.Repo, "branch": r.Branch, "path": r.Path, "absolute_path": cfg.Abs(r.Path), "checked_out": r.CheckedOut, "status": r.StatusLine, "dirty_count": r.DirtyCount})
		}
		return map[string]any{"manifest": m, "manifest_path": cfg.ManifestPath(m.Feature), "directory": cfg.FeatureDir(m.Feature), "worktrees": worktrees}, nil
	})
	addTool(s, "feature_create", "Create an empty super-feature manifest, local state and starting targets preset. Run repos_pull first so all source clones are ready.", write, func(_ context.Context, in createFeatureInput) (any, error) {
		cfg, _, err := s.project(in.ProjectInput)
		if err != nil {
			return nil, err
		}
		if err := component(in.Name); err != nil {
			return nil, err
		}
		if in.Shorthand != "" {
			if err := component(in.Shorthand); err != nil {
				return nil, err
			}
		}
		if err := safeFeaturePaths(cfg, in.Name, nil); err != nil {
			return nil, err
		}
		if err := superfeature.Create(cfg, in.Name, in.Shorthand, in.Description); err != nil {
			return nil, err
		}
		return manifest.Load(cfg.ManifestPath(in.Name))
	})
	addTool(s, "feature_update", "Edit shared description/action variables or local display name. Does not rename the feature slug, folders or existing branches.", write, func(_ context.Context, in updateFeatureInput) (any, error) {
		cfg, _, m, err := s.feature(in.FeatureInput)
		if err != nil {
			return nil, err
		}
		if in.DisplayName != nil && strings.TrimSpace(*in.DisplayName) == "" {
			return nil, fmt.Errorf("display_name cannot be empty")
		}
		if in.Description != nil {
			m.Description = *in.Description
		}
		if in.Vars != nil {
			m.Vars = *in.Vars
		}
		if in.Description != nil || in.Vars != nil {
			if err := manifest.Save(cfg.ManifestPath(m.Feature), m); err != nil {
				return nil, err
			}
		}
		if in.DisplayName != nil {
			st, err := config.LoadState(cfg.StateFile)
			if err != nil {
				return m, err
			}
			st.EnsureFeature(m.ID, m.Feature)
			f := st.Features[m.ID]
			f.Name = *in.DisplayName
			st.Features[m.ID] = f
			if err := config.SaveState(cfg.StateFile, st); err != nil {
				return m, err
			}
		}
		return m, nil
	})
	addTool(s, "feature_add_worktree", "Add a repo/branch checkout to a feature. New branch creation requires a successful origin fetch. base_source chooses the fetched origin ref, the local ref, or fast-forwarding origin into local first. Multiple branches of one repo are supported; existing_branch attaches explicitly.", network, func(ctx context.Context, in addWorktreeInput) (any, error) {
		cfg, pd, m, err := s.feature(in.FeatureInput)
		if err != nil {
			return nil, err
		}
		found := false
		for _, r := range pd.Repos {
			if r.Name == in.Repo {
				found = true
			}
		}
		if !found {
			return nil, fmt.Errorf("unknown repo %q", in.Repo)
		}
		w, err := superfeature.AddContext(ctx, cfg, pd, m.Feature, superfeature.AddSpec{Repo: in.Repo, Sub: in.Branch, From: in.From, BaseSource: in.BaseSource, OmitFeaturePrefix: in.NoFeaturePrefix, ExistingBranch: in.ExistingBranch})
		if err != nil {
			return w, err
		}
		return map[string]any{"worktree": w, "absolute_path": cfg.Abs(w.Path)}, nil
	})
	addTool(s, "feature_remove_worktree", "Force-remove one recorded checkout and its manifest entry. Discards uncommitted checkout changes; optionally deletes its local branch.", remove, func(_ context.Context, in removeWorktreeInput) (any, error) {
		cfg, _, m, err := s.feature(in.FeatureInput)
		if err != nil {
			return nil, err
		}
		if in.Branch == "" || m.Find(in.Repo, in.Branch) < 0 {
			return nil, fmt.Errorf("repo and branch must match a recorded worktree")
		}
		return superfeature.Remove(cfg, m.Feature, superfeature.RemoveSpec{Repo: in.Repo, Branch: in.Branch, PruneBranch: in.DeleteBranch})
	})
	addTool(s, "feature_up", "Rebuild recorded checkouts. Refuses outdated folder names until feature_folders_apply resolves them. Missing branches are skipped unless create_missing_branches is true. Known branches can rebuild from cached refs offline; inventing a missing branch requires a successful fetch.", network, func(ctx context.Context, in struct {
		FeatureInput
		CreateMissingBranches bool `json:"create_missing_branches,omitempty"`
	}) (any, error) {
		cfg, _, m, err := s.feature(in.FeatureInput)
		if err != nil {
			return nil, err
		}
		return superfeature.UpContext(ctx, cfg, m.Feature, func(string, string) bool { return in.CreateMissingBranches })
	})
	addTool(s, "feature_down", "Force-remove all recorded feature checkouts, discarding uncommitted changes. Keeps the manifest, branches and unrecorded checkouts.", remove, func(_ context.Context, in FeatureInput) (any, error) {
		cfg, _, m, err := s.feature(in)
		if err != nil {
			return nil, err
		}
		return superfeature.Down(cfg, m.Feature)
	})
	addTool(s, "feature_delete", "Delete a feature manifest, local state and all its recorded checkouts. Discards uncommitted changes. Optionally force-delete local branches. Unrecorded checkouts survive.", remove, func(_ context.Context, in struct {
		FeatureInput
		DeleteBranches bool `json:"delete_branches"`
	}) (any, error) {
		cfg, _, m, err := s.feature(in.FeatureInput)
		if err != nil {
			return nil, err
		}
		return nil, superfeature.Delete(cfg, m.Feature, in.DeleteBranches)
	})
	addTool(s, "feature_teardown", "Delete a feature record using explicit per-worktree choices for checkout removal, file deletion and branch deletion. Supply every recorded path exactly once. False choices preserve those artifacts on disk.", remove, func(_ context.Context, in struct {
		FeatureInput
		Choices []teardownChoice `json:"choices"`
	}) (any, error) {
		cfg, _, m, err := s.feature(in.FeatureInput)
		if err != nil {
			return nil, err
		}
		if len(in.Choices) != len(m.Worktrees) {
			return nil, fmt.Errorf("supply one choice per recorded worktree from feature_get")
		}
		byPath := map[string]models.Worktree{}
		for _, w := range m.Worktrees {
			byPath[w.Path] = w
		}
		plan := []superfeature.RepoTeardown{}
		for _, c := range in.Choices {
			w, ok := byPath[c.Path]
			if !ok {
				return nil, fmt.Errorf("unknown or repeated worktree path %q; refresh feature_get", c.Path)
			}
			delete(byPath, c.Path)
			plan = append(plan, superfeature.RepoTeardown{Repo: w.Repo, Branch: w.Branch, Path: w.Path, RemoveWorktree: c.RemoveWorktree, DeleteFiles: c.DeleteFiles, DeleteBranch: c.DeleteBranch})
		}
		return superfeature.DeleteWalk(cfg, m.Feature, plan)
	})
	addTool(s, "feature_relink", "Refresh a feature's local back-link to this super-repo and data directory.", write, func(_ context.Context, in FeatureInput) (any, error) {
		cfg, _, m, err := s.feature(in)
		if err != nil {
			return nil, err
		}
		return map[string]any{"path": cfg.FeatureLinkPath(m.Feature)}, config.WriteFeatureLink(cfg, m.Feature)
	})
	addTool(s, "feature_folders_check", "Inspect outdated worktree folder names and propose repo--branch paths. Does not change anything. Echo each returned change with an explicit rename boolean to feature_folders_apply.", readOnly, func(_ context.Context, in FeatureInput) (any, error) {
		cfg, _, m, err := s.feature(in)
		if err != nil {
			return nil, err
		}
		return superfeature.CheckFolderNames(cfg, m.Feature)
	})
	addTool(s, "feature_folders_apply", "Resolve every current folder-name change. rename=true moves the checkout and remaps targets/presets. rename=false drops only its manifest entry and keeps checkout and branch. Echo the complete current feature_folders_check data; stale plans fail.", remove, func(_ context.Context, in struct {
		FeatureInput
		Decisions []superfeature.FolderDecision `json:"decisions"`
	}) (any, error) {
		cfg, _, m, err := s.feature(in.FeatureInput)
		if err != nil {
			return nil, err
		}
		return superfeature.ApplyFolderNames(cfg, m.Feature, in.Decisions)
	})
	addTool(s, "feature_diagnose", "Inspect unrecorded worktrees and missing checkouts. Use feature_reconcile with explicit path decisions to repair them.", readOnly, func(_ context.Context, in FeatureInput) (any, error) {
		cfg, pd, m, err := s.feature(in)
		if err != nil {
			return nil, err
		}
		return superfeature.Diagnose(cfg, pd, m.Feature)
	})
	addTool(s, "feature_reconcile", "Repair selected drift paths from feature_diagnose. adopt records an orphan; remove deletes it including dirty files; rebuild restores a missing checkout and may create its missing branch; drop removes only a missing manifest entry. Unselected paths remain unchanged.", script, func(ctx context.Context, in struct {
		FeatureInput
		Choices []repairChoice `json:"choices"`
	}) (any, error) {
		cfg, pd, m, err := s.feature(in.FeatureInput)
		if err != nil {
			return nil, err
		}
		d, err := superfeature.Diagnose(cfg, pd, m.Feature)
		if err != nil {
			return nil, err
		}
		orphans := map[string]superfeature.Orphan{}
		missing := map[string]models.Worktree{}
		for _, o := range d.Orphans {
			orphans[o.Path] = o
		}
		for _, w := range d.Missing {
			missing[w.Path] = w
		}
		seen := map[string]bool{}
		for _, c := range in.Choices {
			_, orphan := orphans[c.Path]
			_, absent := missing[c.Path]
			if seen[c.Path] || !(orphan && (c.Action == "adopt" || c.Action == "remove") || absent && (c.Action == "rebuild" || c.Action == "drop")) {
				return nil, fmt.Errorf("invalid or stale decision for %q; refresh feature_diagnose", c.Path)
			}
			seen[c.Path] = true
		}
		rows := []map[string]any{}
		var failures []error
		for _, c := range in.Choices {
			if err := ctx.Err(); err != nil {
				return rows, err
			}
			var err error
			switch c.Action {
			case "adopt":
				err = superfeature.AdoptOrphan(cfg, m.Feature, orphans[c.Path])
			case "remove":
				err = superfeature.RemoveOrphan(cfg, orphans[c.Path])
			case "rebuild":
				err = superfeature.RebuildMissingContext(ctx, cfg, missing[c.Path])
			case "drop":
				err = superfeature.DropMissing(cfg, m.Feature, missing[c.Path])
			}
			row := map[string]any{"path": c.Path, "action": c.Action, "success": err == nil}
			if err != nil {
				row["error"] = err.Error()
				failures = append(failures, err)
			}
			rows = append(rows, row)
		}
		return rows, errors.Join(failures...)
	})
}
