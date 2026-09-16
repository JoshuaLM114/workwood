package mcpserver

import (
	"context"
	"fmt"
	"strings"

	"github.com/JoshuaLM114/workwood/libs/gitx"
	"github.com/JoshuaLM114/workwood/models"
	"github.com/JoshuaLM114/workwood/projectdef"
	"github.com/JoshuaLM114/workwood/repos"
)

type RepoInput struct {
	ProjectInput
	Repo string `json:"repo" jsonschema:"Configured repository name."`
}

type repoAddInput struct {
	ProjectInput
	Name          string `json:"name"`
	URL           string `json:"url" jsonschema:"Git clone URL or absolute local repository path."`
	DefaultBranch string `json:"default_branch"`
}

type repoDefaultInput struct {
	RepoInput
	Branch string `json:"branch"`
}

func (s *Server) addRepoTools() {
	addTool(s, "repos_list", "List source repositories, clone paths, active branches and cached upstream status. Use repos_fetch or repos_pull to refresh remote information.", readOnly, func(_ context.Context, in ProjectInput) (any, error) {
		cfg, pd, err := s.project(in)
		if err != nil {
			return nil, err
		}
		rows := make([]map[string]any, 0, len(pd.Repos))
		for _, r := range pd.Repos {
			dir := cfg.BaseRepo(r.Name)
			sync := repos.AheadBehind(dir)
			rows = append(rows, map[string]any{"name": r.Name, "url": r.URL, "default_branch": r.DefaultBranch, "path": dir, "cloned": repos.ClassifyClone(dir) == repos.StateClone, "active_branch": repos.ActiveBranch(dir), "ahead": sync.Ahead, "behind": sync.Behind, "has_upstream": sync.HasUpstream})
		}
		return rows, nil
	})
	addTool(s, "repo_add", "Add a repository to workwood.yml. Does not clone it; use repos_pull next.", write, func(ctx context.Context, in repoAddInput) (any, error) {
		cfg, pd, err := s.project(in.ProjectInput)
		if err != nil {
			return nil, err
		}
		if err := component(in.Name); err != nil {
			return nil, err
		}
		if strings.TrimSpace(in.URL) == "" || strings.HasPrefix(in.URL, "-") {
			return nil, fmt.Errorf("url must be a non-empty Git URL or local path")
		}
		for _, r := range pd.Repos {
			if r.Name == in.Name {
				return nil, fmt.Errorf("repo %q already exists", in.Name)
			}
		}
		if _, err := gitx.RunContext(ctx, "", "check-ref-format", "refs/heads/"+in.DefaultBranch); err != nil || in.DefaultBranch == "HEAD" || strings.HasPrefix(in.DefaultBranch, "-") {
			return nil, fmt.Errorf("invalid default_branch %q", in.DefaultBranch)
		}
		r := models.Repo{Name: in.Name, URL: in.URL, DefaultBranch: in.DefaultBranch}
		pd.Repos = append(pd.Repos, r)
		return r, projectdef.Save(cfg.ProjectDef, pd)
	})
	addTool(s, "repo_remove", "Remove a repository definition from workwood.yml. Keeps its source clone, branches and feature checkouts on disk.", remove, func(_ context.Context, in RepoInput) (any, error) {
		cfg, pd, err := s.project(in.ProjectInput)
		if err != nil {
			return nil, err
		}
		for i, r := range pd.Repos {
			if r.Name == in.Repo {
				pd.Repos = append(pd.Repos[:i], pd.Repos[i+1:]...)
				return map[string]any{"removed": in.Repo, "checkout_kept": true}, projectdef.Save(cfg.ProjectDef, pd)
			}
		}
		return nil, fmt.Errorf("unknown repo %q", in.Repo)
	})
	addTool(s, "repo_set_default_branch", "Change a repo's default branch and switch its base clone if present. Does not pull; use repos_pull to fast-forward it.", write, func(ctx context.Context, in repoDefaultInput) (any, error) {
		cfg, pd, err := s.project(in.ProjectInput)
		if err != nil {
			return nil, err
		}
		if in.Branch == "HEAD" || strings.HasPrefix(in.Branch, "-") {
			return nil, fmt.Errorf("invalid branch %q", in.Branch)
		}
		if _, err := gitx.RunContext(ctx, "", "check-ref-format", "refs/heads/"+in.Branch); err != nil {
			return nil, err
		}
		for i, r := range pd.Repos {
			if r.Name != in.Repo {
				continue
			}
			dir := cfg.BaseRepo(r.Name)
			if repos.ClassifyClone(dir) == repos.StateClone {
				if _, err := gitx.RunContext(ctx, dir, "checkout", in.Branch); err != nil {
					return nil, err
				}
			}
			pd.Repos[i].DefaultBranch = in.Branch
			return pd.Repos[i], projectdef.Save(cfg.ProjectDef, pd)
		}
		return nil, fmt.Errorf("unknown repo %q", in.Repo)
	})
	addTool(s, "repo_branches", "List local and cached origin branches for a cloned repo, or query remote heads for a missing clone. Use repos_fetch to refresh an existing clone first.", effects{readOnly: true, idempotent: true, openWorld: true}, func(ctx context.Context, in RepoInput) (any, error) {
		cfg, pd, err := s.project(in.ProjectInput)
		if err != nil {
			return nil, err
		}
		for _, r := range pd.Repos {
			if r.Name != in.Repo {
				continue
			}
			dir := cfg.BaseRepo(r.Name)
			if repos.ClassifyClone(dir) == repos.StateClone {
				if _, err := gitx.RunContext(ctx, dir, "rev-parse", "--git-dir"); err != nil {
					return nil, err
				}
				return repos.Branches(dir), nil
			}
			out, err := gitx.RunContext(ctx, "", "ls-remote", "--heads", "--", r.URL)
			if err != nil {
				return nil, err
			}
			branches := []repos.BranchRef{}
			for _, line := range strings.Split(out, "\n") {
				if _, name, found := strings.Cut(line, "\trefs/heads/"); found {
					branches = append(branches, repos.BranchRef{Name: name, Remote: true})
				}
			}
			return branches, nil
		}
		return nil, fmt.Errorf("unknown repo %q", in.Repo)
	})
	addTool(s, "repos_pull", "Clone missing source repos, fetch remotes, check out each default branch and fast-forward it. Reports partial progress and fails on checkout, network or pull errors. Does not update feature branches.", network, func(ctx context.Context, in ProjectInput) (any, error) {
		cfg, pd, err := s.project(in)
		if err != nil {
			return nil, err
		}
		return repos.SyncContext(ctx, cfg, pd)
	})
	addTool(s, "repos_fetch", "Refresh remote refs for every existing source clone without checking out or pulling branches. Missing clones are reported as skipped.", network, func(ctx context.Context, in ProjectInput) (any, error) {
		cfg, pd, err := s.project(in)
		if err != nil {
			return nil, err
		}
		rows := []map[string]any{}
		for _, r := range pd.Repos {
			dir := cfg.BaseRepo(r.Name)
			if repos.ClassifyClone(dir) != repos.StateClone {
				rows = append(rows, map[string]any{"repo": r.Name, "skipped": true})
				continue
			}
			if _, err := gitx.RunContext(ctx, dir, "fetch", "--all", "--prune"); err != nil {
				return rows, fmt.Errorf("%s: %w", r.Name, err)
			}
			rows = append(rows, map[string]any{"repo": r.Name, "fetched": true})
		}
		return rows, nil
	})
}
