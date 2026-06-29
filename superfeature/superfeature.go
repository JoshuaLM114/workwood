// Package superfeature implements the super-feature operations: create a
// manifest, add/remove worktrees, rebuild from a manifest, report status, and
// tear down — plus running an action against the feature's selected targets.
//
// A super-feature is a tracked manifest recording every worktree+branch that
// belongs to it. Worktrees are cut from the base clones in the project's
// main_dir, so one feature can hold MANY worktrees of the SAME repo on different
// branches — the whole reason this tool exists instead of git submodules.
package superfeature

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/JoshuaLM114/workwood/action"
	"github.com/JoshuaLM114/workwood/config"
	"github.com/JoshuaLM114/workwood/gitx"
	"github.com/JoshuaLM114/workwood/i18n"
	"github.com/JoshuaLM114/workwood/manifest"
	"github.com/JoshuaLM114/workwood/projectdef"
	"github.com/JoshuaLM114/workwood/repos"
	"github.com/JoshuaLM114/workwood/targetcfg"
	"github.com/google/uuid"
)

// CheckReposReady errors when the project has no repos defined, or any configured
// repo isn't a real main clone yet — the precondition for creating super-features
// (worktrees are cut from those clones).
func CheckReposReady(cfg *config.Config) error {
	pd, err := projectdef.Load(cfg.ProjectDef)
	if err != nil {
		return err
	}
	if len(pd.Repos) == 0 {
		return i18n.Err("err.no_repos_defined")
	}
	if bad := repos.Unready(cfg, pd); len(bad) > 0 {
		return i18n.Err("err.repos_not_ready", strings.Join(bad, ", "))
	}
	return nil
}

// Repos returns the distinct repo names in a feature's manifest, in order.
func Repos(cfg *config.Config, slug string) ([]string, error) {
	m, err := manifest.Load(cfg.ManifestPath(slug))
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var out []string
	for _, w := range m.Worktrees {
		if !seen[w.Repo] {
			seen[w.Repo] = true
			out = append(out, w.Repo)
		}
	}
	return out, nil
}

// checkProject errors when a manifest's parent UUID disagrees with the resolved
// project — i.e. a manifest copied in from a different super-repo.
func checkProject(cfg *config.Config, m *manifest.Manifest) error {
	if m.Project != "" && m.Project != cfg.ProjectID {
		return i18n.Err("err.manifest_wrong_project", m.Feature, m.Project, cfg.ProjectID)
	}
	return nil
}

// actionSet resolves the target set + manifest vars for running/initing an action
// against a feature (validating the manifest's project + UUID, self-healing the
// back-link). With override==nil it uses the feature's persisted working set.
func actionSet(cfg *config.Config, slug string, override targetcfg.Set) (targetcfg.Set, map[string]string, error) {
	m, err := manifest.Load(cfg.ManifestPath(slug))
	if err != nil {
		return nil, nil, err
	}
	if err := checkProject(cfg, m); err != nil {
		return nil, nil, err
	}
	if m.ID == "" {
		return nil, nil, i18n.Err("err.feature_not_adopted", slug)
	}
	_, _ = config.EnsureFeatureLink(cfg, slug)
	set := override
	if set == nil {
		pd, perr := projectdef.Load(cfg.ProjectDef)
		if perr != nil {
			return nil, nil, perr
		}
		if set, err = targetcfg.Working(cfg, pd, m); err != nil {
			return nil, nil, err
		}
	}
	return set, m.Vars, nil
}

// RunAction runs the named action against a feature's targets. With override==nil
// it uses the feature's persisted working set; pass a non-nil Set to run against
// an explicit configuration (e.g. a loaded preset).
func RunAction(cfg *config.Config, slug, name string, override targetcfg.Set) error {
	set, vars, err := actionSet(cfg, slug, override)
	if err != nil {
		return err
	}
	return action.Run(cfg, slug, name, set, vars)
}

// InitAction runs the action's Init — its bootstrap that creates the minimal files
// it needs in the selected targets (idempotent). Returns the action's output.
func InitAction(cfg *config.Config, slug, name string, override targetcfg.Set) (string, error) {
	set, vars, err := actionSet(cfg, slug, override)
	if err != nil {
		return "", err
	}
	return action.Init(cfg, slug, name, set, vars)
}

// ResolveBranch builds the full git branch for a worktree-branch-name. The real
// branch is <prefix>/<sub> (prefix = the feature's shorthand); if the caller
// already typed that prefix it isn't doubled. sub may contain slashes (fix/login).
func ResolveBranch(prefix, sub string) string {
	return ResolveBranchWith(prefix, sub, false)
}

// ResolveBranchWith is ResolveBranch with an override: when omitPrefix is true the
// <prefix>/ is dropped, so the branch is the raw <sub> (a standalone branch).
func ResolveBranchWith(prefix, sub string, omitPrefix bool) string {
	if omitPrefix {
		return sub
	}
	if sub == prefix || strings.HasPrefix(sub, prefix+"/") {
		return sub
	}
	return prefix + "/" + sub
}

// Create writes a starter manifest for a new super-feature: it mints the feature
// UUID, links it to the project, and records it in this developer's state
// (active_name defaulting to the slug). It refuses to clobber an existing feature,
// and refuses to create one until the project's base repos are real clones (so
// worktrees can actually be cut from them).
// validName rejects a feature slug or repo name that could escape its directory
// when joined into a path: empty, a "."/".." component, a path separator, or a
// leading "-" (git could misread it as a flag).
func validName(s string) error {
	if s == "" || s == "." || s == ".." || strings.ContainsAny(s, `/\`) || strings.HasPrefix(s, "-") {
		return i18n.Err("err.invalid_name", s)
	}
	return nil
}

// underData reports whether abs is strictly inside the data dir's features/ tree.
func underData(cfg *config.Config, abs string) bool {
	rel, err := filepath.Rel(cfg.FeaturesDir, filepath.Clean(abs))
	if err != nil {
		return false
	}
	return rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// safeRemoveAll removes abs only when it resolves inside the features tree — the
// last-line guard so a corrupt or hand-edited manifest path can never delete
// outside $WORKWOOD_DATA.
func safeRemoveAll(cfg *config.Config, abs string) error {
	if !underData(cfg, abs) {
		return i18n.Err("err.unsafe_path", abs)
	}
	return os.RemoveAll(abs)
}

func Create(cfg *config.Config, slug, shorthand, desc string) error {
	if err := validName(slug); err != nil {
		return err
	}
	if err := CheckReposReady(cfg); err != nil {
		return err
	}
	path := cfg.ManifestPath(slug)
	if _, err := os.Stat(path); err == nil {
		return i18n.Err("err.manifest_exists", path)
	}
	if err := os.MkdirAll(cfg.FeatureDir(slug), 0o755); err != nil {
		return err
	}
	shorthand = strings.TrimSpace(shorthand)
	if shorthand == "" {
		shorthand = manifest.DefaultShorthand(slug)
	}
	m := &manifest.Manifest{
		ID:          uuid.NewString(),
		Project:     cfg.ProjectID,
		Feature:     slug,
		Shorthand:   shorthand,
		Description: desc,
		Created:     time.Now().Format("2006-01-02"),
		Worktrees:   []manifest.Worktree{},
	}
	if err := manifest.Save(path, m); err != nil {
		return err
	}
	// Drop the back-link so the tool can later be run from this feature folder.
	if err := config.WriteFeatureLink(cfg, slug); err != nil {
		return err
	}
	// Seed a starting targets preset named after the feature (repos only — no
	// worktrees yet). Best-effort: the user can regenerate it after adding worktrees
	// (`workwood targets generate`), so a hiccup here shouldn't fail Create.
	if pd, e := projectdef.Load(cfg.ProjectDef); e == nil {
		_ = targetcfg.SavePreset(cfg, slug, targetcfg.CleanSet(cfg, pd, m))
	}
	st, err := config.LoadState(cfg.StateFile)
	if err != nil {
		return err
	}
	st.EnsureFeature(m.ID, slug)
	return config.SaveState(cfg.StateFile, st)
}

// AddSpec describes a worktree the caller wants created.
type AddSpec struct {
	Repo string // base repo name (which clone to cut the worktree from)
	Sub  string // worktree-branch-name (the part after <feature>/)
	From string // source branch a NEW branch is cut from ("" → repo default)
	// OmitFeaturePrefix drops the <feature>/ prefix, yielding the raw <sub> as
	// the branch instead of <feature>/<sub>.
	OmitFeaturePrefix bool
}

// Add provisions one worktree for an existing feature and records it in the
// manifest.
func Add(cfg *config.Config, pd *projectdef.File, name string, spec AddSpec) (manifest.Worktree, error) {
	if err := validName(name); err != nil {
		return manifest.Worktree{}, err
	}
	path := cfg.ManifestPath(name)
	m, err := manifest.Load(path)
	if err != nil {
		return manifest.Worktree{}, i18n.Errw(err, "err.no_manifest_create", path)
	}
	wt, err := provision(cfg, pd, m, spec)
	if err != nil {
		return manifest.Worktree{}, err
	}
	m.Worktrees = append(m.Worktrees, wt)
	if err := manifest.Save(path, m); err != nil {
		return wt, err
	}
	return wt, nil
}

// provision creates the actual worktree (without saving the manifest) and
// returns the entry to record. It carries every guard: branch prefixing, the
// ref-hierarchy guard, attach-to-existing precedence, and directory-collision
// suffixing.
func provision(cfg *config.Config, pd *projectdef.File, m *manifest.Manifest, spec AddSpec) (manifest.Worktree, error) {
	if err := validName(spec.Repo); err != nil {
		return manifest.Worktree{}, err
	}
	branch := ResolveBranchWith(m.BranchPrefix(), spec.Sub, spec.OmitFeaturePrefix)
	baseRepo := cfg.BaseRepo(spec.Repo)
	if !gitx.IsRepo(baseRepo) {
		return manifest.Worktree{}, i18n.Err("err.base_repo_missing", baseRepo)
	}

	// Where a NEW branch starts from: explicit --from, else the repo's configured
	// default_branch, else HEAD. (Ignored when attaching to an existing branch.)
	base := spec.From
	if base == "" {
		base = pd.DefaultBranch(spec.Repo)
	}
	if base == "" {
		base = "HEAD"
	}

	// Refresh remote refs so we attach to a teammate's pushed branch / fresh base.
	gitx.Fetch(baseRepo)

	if err := checkRefHierarchy(baseRepo, branch); err != nil {
		return manifest.Worktree{}, err
	}

	rel := allocPath(cfg, m, spec.Repo, branch)
	abs := cfg.Abs(rel)
	if err := addWorktree(baseRepo, branch, base, abs); err != nil {
		return manifest.Worktree{}, err
	}
	return manifest.Worktree{Repo: spec.Repo, Branch: branch, Base: base, Path: rel}, nil
}

// addWorktree attaches to whatever branch source exists, in priority order:
// local branch → origin branch → a new branch cut from base (--no-track).
func addWorktree(baseRepo, branch, base, abs string) error {
	switch {
	case gitx.HasLocalBranch(baseRepo, branch):
		return gitx.AddWorktreeExistingLocal(baseRepo, abs, branch)
	case gitx.HasRemoteBranch(baseRepo, branch):
		return gitx.AddWorktreeTrackRemote(baseRepo, abs, branch)
	default:
		baseref := base
		if gitx.HasRemoteBranch(baseRepo, base) {
			baseref = "origin/" + base
		}
		return gitx.AddWorktreeNewBranch(baseRepo, abs, branch, baseref)
	}
}

// checkRefHierarchy enforces git's rule that a ref 'x' cannot coexist with 'x/y'
// (refs are files on disk). It only matters when CREATING a new branch.
func checkRefHierarchy(baseRepo, branch string) error {
	if gitx.HasLocalBranch(baseRepo, branch) || gitx.HasRemoteBranch(baseRepo, branch) {
		return nil
	}
	branches, err := gitx.LocalBranches(baseRepo)
	if err != nil {
		return err
	}
	for _, b := range branches {
		if b == "" {
			continue
		}
		if strings.HasPrefix(branch, b+"/") {
			return i18n.Err("err.ref_blocks", b, branch)
		}
		if strings.HasPrefix(b, branch+"/") {
			return i18n.Err("err.ref_nests", b, branch)
		}
	}
	return nil
}

// allocPath chooses the features_dir-relative worktree dir for a new entry:
// <feature>/<repo>, or suffixed with the branch slug when that dir is already
// taken (a 2nd branch of the same repo).
func allocPath(cfg *config.Config, m *manifest.Manifest, repo, branch string) string {
	dir := repo
	if dirTaken(cfg, m, m.Feature+"/"+dir) {
		// On a 2nd worktree of the same repo, suffix the dir with the branch's
		// sub-name (the part after the branch prefix) so the folders stay distinct.
		sub := strings.TrimPrefix(branch, m.BranchPrefix()+"/")
		dir = repo + "--" + slugify(sub)
	}
	return m.Feature + "/" + dir
}

// dirTaken reports whether a features_dir-relative path is already used, either
// on disk or by an entry already in the manifest.
func dirTaken(cfg *config.Config, m *manifest.Manifest, rel string) bool {
	if _, err := os.Stat(cfg.Abs(rel)); err == nil {
		return true
	}
	for _, w := range m.Worktrees {
		if w.Path == rel {
			return true
		}
	}
	return false
}

// slugify turns a branch sub-name into a short filesystem-safe dir suffix:
// '/' and ' ' become '_', then only [alnum . _ -] survive.
func slugify(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '/' || r == ' ':
			b.WriteByte('_')
		case unicode.IsLetter(r) || unicode.IsDigit(r) || r == '.' || r == '_' || r == '-':
			b.WriteRune(r)
		}
	}
	return b.String()
}

// Up rebuilds every worktree a manifest records (idempotent) — how a fresh
// checkout reconstructs the exact feature set after `repos pull`.
//
// For a worktree whose branch exists on NEITHER the local repo nor origin (after
// fetching), rebuilding would invent a brand-new local branch. onNew, if non-nil,
// is asked first — return false to skip that worktree instead of creating it. A
// nil onNew creates them without asking (the non-interactive default).
func Up(cfg *config.Config, name string, onNew func(repo, branch string) bool) ([]string, error) {
	path := cfg.ManifestPath(name)
	m, err := manifest.Load(path)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(cfg.FeatureDir(name), 0o755); err != nil {
		return nil, err
	}
	// Refresh the back-link (also (re)written here so a fresh checkout that rebuilds
	// from the manifest gets a link pointing at THIS developer's super-repo).
	if err := config.WriteFeatureLink(cfg, name); err != nil {
		return nil, err
	}
	var log []string
	for _, w := range m.Worktrees {
		baseRepo := cfg.BaseRepo(w.Repo)
		abs := cfg.Abs(w.Path)
		if !gitx.IsRepo(baseRepo) {
			log = append(log, i18n.T("log.skip_base_missing", w.Repo))
			continue
		}
		if _, err := os.Stat(abs); err == nil {
			log = append(log, i18n.T("log.exists", w.Path))
			continue
		}
		gitx.Fetch(baseRepo)
		// No existing branch anywhere → this would create a new local branch.
		if onNew != nil &&
			!gitx.HasLocalBranch(baseRepo, w.Branch) &&
			!gitx.HasRemoteBranch(baseRepo, w.Branch) &&
			!onNew(w.Repo, w.Branch) {
			log = append(log, i18n.T("log.skip_no_remote", w.Repo, w.Branch))
			continue
		}
		if err := addWorktree(baseRepo, w.Branch, w.Base, abs); err != nil {
			return log, err
		}
		log = append(log, i18n.T("log.up", w.Repo, w.Branch, w.Path))
	}
	return log, nil
}

// Down detaches every worktree (branches kept) and drops the feature dir. The
// manifest is kept so it can be rebuilt.
func Down(cfg *config.Config, name string) ([]string, error) {
	if err := validName(name); err != nil {
		return nil, err
	}
	path := cfg.ManifestPath(name)
	m, err := manifest.Load(path)
	if err != nil {
		return nil, err
	}
	var log []string
	for _, w := range m.Worktrees {
		removeWorktreeDir(cfg, w.Repo, w.Path)
		log = append(log, i18n.T("log.down", w.Path))
	}
	_ = safeRemoveAll(cfg, cfg.FeatureDir(name))
	return log, nil
}

// removeWorktreeDir removes a worktree via git, falling back to rm -rf when git
// doesn't know about it (or the base clone is gone).
func removeWorktreeDir(cfg *config.Config, repo, relPath string) {
	baseRepo := cfg.BaseRepo(repo)
	abs := cfg.Abs(relPath)
	if gitx.IsRepo(baseRepo) {
		if err := gitx.RemoveWorktree(baseRepo, abs); err == nil {
			return
		}
	}
	_ = safeRemoveAll(cfg, abs)
}

// RemoveSpec identifies a single worktree to remove and whether to also delete
// its local branch.
type RemoveSpec struct {
	Repo        string
	Sub         string // optional; disambiguates when a repo has multiple worktrees
	Branch      string // optional exact branch (used by the editor; bypasses Sub→branch resolution)
	PruneBranch bool
}

// Remove drops a SINGLE worktree from a feature (vs. Down which removes all).
func Remove(cfg *config.Config, name string, spec RemoveSpec) (string, error) {
	path := cfg.ManifestPath(name)
	m, err := manifest.Load(path)
	if err != nil {
		return "", err
	}

	want := ""
	if spec.Sub != "" {
		want = ResolveBranch(m.BranchPrefix(), spec.Sub)
	}

	var matches []manifest.Worktree
	for _, w := range m.Worktrees {
		if w.Repo != spec.Repo {
			continue
		}
		if want != "" && w.Branch != want {
			continue
		}
		matches = append(matches, w)
	}

	switch len(matches) {
	case 0:
		if want != "" {
			return "", i18n.Err("err.no_worktree_branch", spec.Repo, want, name)
		}
		return "", i18n.Err("err.no_worktree", spec.Repo, name)
	case 1:
		// ok
	default:
		var b strings.Builder
		b.WriteString(i18n.T("err.multiple_worktrees", spec.Repo, name))
		for _, w := range matches {
			b.WriteString(i18n.T("err.multiple_worktrees_row", strings.TrimPrefix(w.Branch, m.BranchPrefix()+"/"), w.Branch))
		}
		return "", errors.New(b.String())
	}

	w := matches[0]
	if err := RemoveBranchEntry(cfg, m, w, spec.PruneBranch); err != nil {
		return "", err
	}
	if err := manifest.Save(path, m); err != nil {
		return "", err
	}
	return i18n.T("log.removed_worktree", w.Path, w.Repo, w.Branch), nil
}

// RemoveBranchEntry removes a worktree's checkout, drops it from m (in memory),
// and optionally deletes its local branch. The caller saves the manifest.
func RemoveBranchEntry(cfg *config.Config, m *manifest.Manifest, w manifest.Worktree, prune bool) error {
	removeWorktreeDir(cfg, w.Repo, w.Path)
	idx := m.Find(w.Repo, w.Branch)
	if idx >= 0 {
		m.Worktrees = append(m.Worktrees[:idx], m.Worktrees[idx+1:]...)
	}
	if prune {
		baseRepo := cfg.BaseRepo(w.Repo)
		if gitx.IsRepo(baseRepo) {
			_ = gitx.DeleteBranch(baseRepo, w.Branch)
		}
	}
	return nil
}

// Delete tears down all worktrees, optionally prunes the branches, and removes
// the manifest.
func Delete(cfg *config.Config, name string, pruneBranches bool) error {
	if err := validName(name); err != nil {
		return err
	}
	path := cfg.ManifestPath(name)
	m, err := manifest.Load(path)
	if err != nil {
		return err
	}
	if _, err := Down(cfg, name); err != nil {
		return err
	}
	if pruneBranches {
		for _, w := range m.Worktrees {
			baseRepo := cfg.BaseRepo(w.Repo)
			if gitx.IsRepo(baseRepo) {
				_ = gitx.DeleteBranch(baseRepo, w.Branch)
			}
		}
	}
	// Drop this feature's entry from the developer's state (best-effort).
	if st, e := config.LoadState(cfg.StateFile); e == nil {
		delete(st.Features, m.ID)
		_ = config.SaveState(cfg.StateFile, st)
	}
	return os.Remove(path)
}

// RepoTeardown is the per-repo deletion choice for DeleteWalk. The three flags map
// to the three things a super-feature worktree leaves on disk/in git.
type RepoTeardown struct {
	Repo, Branch, Path string
	RemoveWorktree     bool // `git worktree remove --force`: delete the checkout + unregister it
	DeleteFiles        bool // force `rm -rf` the directory (+ prune) if anything remains
	DeleteBranch       bool // `git branch -D` in the base clone
}

// DeleteWalk deletes a super-feature, honouring a per-repo teardown plan, then
// removes the feature record (back-link, empty feature dir, state entry, manifest).
// It is the guided counterpart to Delete: each worktree's files/registration and
// branch are removed only where the plan says so; kept artifacts are left in place
// (orphaned by the user's choice). Destructive + not reversible. Returns a log.
func DeleteWalk(cfg *config.Config, name string, plan []RepoTeardown) ([]string, error) {
	if err := validName(name); err != nil {
		return nil, err
	}
	path := cfg.ManifestPath(name)
	m, err := manifest.Load(path)
	if err != nil {
		return nil, err
	}

	var log []string
	for _, p := range plan {
		baseRepo := cfg.BaseRepo(p.Repo)
		abs := cfg.Abs(p.Path)
		isRepo := gitx.IsRepo(baseRepo)

		switch {
		case p.RemoveWorktree:
			if isRepo && gitx.RemoveWorktree(baseRepo, abs) == nil {
				log = append(log, i18n.T("log.delwalk.worktree", p.Repo, abs))
			} else { // not git-managed (or git failed) → force-remove the directory
				if err := safeRemoveAll(cfg, abs); err != nil {
					return log, err
				}
				if isRepo {
					_ = gitx.PruneWorktrees(baseRepo)
				}
				log = append(log, i18n.T("log.delwalk.files", p.Repo, abs))
			}
		case p.DeleteFiles:
			if err := safeRemoveAll(cfg, abs); err != nil {
				return log, err
			}
			if isRepo {
				_ = gitx.PruneWorktrees(baseRepo)
			}
			log = append(log, i18n.T("log.delwalk.files", p.Repo, abs))
		default:
			log = append(log, i18n.T("log.delwalk.kept_files", p.Repo, abs))
		}

		if p.DeleteBranch {
			if isRepo && gitx.DeleteBranch(baseRepo, p.Branch) == nil {
				log = append(log, i18n.T("log.delwalk.branch", p.Repo, p.Branch))
			}
		} else {
			log = append(log, i18n.T("log.delwalk.kept_branch", p.Repo, p.Branch))
		}
	}

	// Remove the super-feature record. The feature dir + its .workwood/ go only if
	// now empty, so any worktrees the user chose to keep survive.
	_ = os.Remove(cfg.FeatureLinkPath(name))
	_ = os.Remove(filepath.Dir(cfg.FeatureLinkPath(name)))
	_ = os.Remove(cfg.FeatureDir(name))
	if st, e := config.LoadState(cfg.StateFile); e == nil {
		delete(st.Features, m.ID)
		_ = config.SaveState(cfg.StateFile, st)
	}
	if err := os.Remove(path); err != nil {
		return log, err
	}
	log = append(log, i18n.T("log.delwalk.record", name))
	return log, nil
}

// StatusRow is one worktree's reported state.
type StatusRow struct {
	Repo       string
	Branch     string
	Path       string
	CheckedOut bool
	StatusLine string
	DirtyCount int
}

// Status reports per-worktree branch + dirty state for a feature.
func Status(cfg *config.Config, name string) (*manifest.Manifest, []StatusRow, error) {
	m, err := manifest.Load(cfg.ManifestPath(name))
	if err != nil {
		return nil, nil, err
	}
	rows := make([]StatusRow, 0, len(m.Worktrees))
	for _, w := range m.Worktrees {
		row := StatusRow{Repo: w.Repo, Branch: w.Branch, Path: w.Path}
		abs := cfg.Abs(w.Path)
		if _, err := os.Stat(abs); err == nil {
			row.CheckedOut = true
			row.StatusLine, _ = gitx.StatusLine(abs)
			row.DirtyCount, _ = gitx.DirtyCount(abs)
		}
		rows = append(rows, row)
	}
	return m, rows, nil
}

// List loads every super-feature manifest.
func List(cfg *config.Config) ([]*manifest.Manifest, error) {
	return manifest.List(cfg.ManifestsDir)
}

// EditResult reports what an ApplyEdit run changed.
type EditResult struct {
	Added   []manifest.Worktree
	Removed []manifest.Worktree
	Log     []string
}

// ApplyEdit is the malleable-edit apply step: against an existing feature it sets
// the description, removes the staged worktrees, and provisions the staged
// additions — then saves the manifest once. This is what makes a future TUI an
// *editor* rather than a one-shot creator.
func ApplyEdit(cfg *config.Config, pd *projectdef.File, name, description string, adds []AddSpec, removes []RemoveSpec) (*EditResult, error) {
	path := cfg.ManifestPath(name)
	m, err := manifest.Load(path)
	if err != nil {
		return nil, err
	}
	res := &EditResult{}
	m.Description = description

	// Persist after EACH on-disk change so a mid-batch failure can't desync the
	// manifest from reality: every worktree we create/remove is recorded before the
	// next op runs. On an error we save what already succeeded, then surface it — the
	// caller keeps the failed item staged to fix + retry.
	for _, rm := range removes {
		branch := rm.Branch
		if branch == "" {
			branch = ResolveBranch(m.BranchPrefix(), rm.Sub)
		}
		idx := m.Find(rm.Repo, branch)
		if idx < 0 {
			res.Log = append(res.Log, i18n.T("log.skip_remove", rm.Repo, branch))
			continue
		}
		w := m.Worktrees[idx]
		if err := RemoveBranchEntry(cfg, m, w, rm.PruneBranch); err != nil {
			_ = manifest.Save(path, m)
			return res, err
		}
		res.Removed = append(res.Removed, w)
		res.Log = append(res.Log, i18n.T("log.removed", w.Path))
		if err := manifest.Save(path, m); err != nil {
			return res, err
		}
	}

	for _, spec := range adds {
		wt, err := provision(cfg, pd, m, spec)
		if err != nil {
			_ = manifest.Save(path, m) // record the worktrees already provisioned
			return res, err
		}
		m.Worktrees = append(m.Worktrees, wt)
		res.Added = append(res.Added, wt)
		res.Log = append(res.Log, i18n.T("log.added", wt.Repo, wt.Branch, wt.Path))
		if err := manifest.Save(path, m); err != nil {
			return res, err
		}
	}

	return res, manifest.Save(path, m) // final save covers a description-only edit
}

// ---- reconcile (doctor): detect + resolve manifest↔disk desyncs ------------

// Orphan is a worktree present on disk under a feature dir but absent from the
// feature's manifest — e.g. an apply died mid-batch before persisting it, or a
// manual checkout. Repo/Branch are best-effort (Repo "" when the dir doesn't map
// to a known base repo).
type Orphan struct {
	Repo, Branch, Base, Path, Abs string
}

// DiagnoseResult reports a feature's drift between its manifest and reality.
type DiagnoseResult struct {
	Feature string
	Orphans []Orphan            // on disk under the feature dir, NOT in the manifest
	Missing []manifest.Worktree // in the manifest, with NO checkout on disk
}

// OK reports whether the feature is in sync.
func (d *DiagnoseResult) OK() bool { return len(d.Orphans) == 0 && len(d.Missing) == 0 }

// Diagnose compares a feature's manifest against what's on disk: worktree dirs not
// recorded (orphans) and recorded worktrees with no checkout (missing). Read-only.
func Diagnose(cfg *config.Config, pd *projectdef.File, name string) (*DiagnoseResult, error) {
	m, err := manifest.Load(cfg.ManifestPath(name))
	if err != nil {
		return nil, err
	}
	res := &DiagnoseResult{Feature: name}
	known := map[string]bool{}
	for _, w := range m.Worktrees {
		known[w.Path] = true
		if _, e := os.Stat(cfg.Abs(w.Path)); errors.Is(e, os.ErrNotExist) {
			res.Missing = append(res.Missing, w)
		}
	}
	repoSet := map[string]bool{}
	for _, r := range pd.Names() {
		repoSet[r] = true
	}
	entries, _ := os.ReadDir(cfg.FeatureDir(name))
	for _, e := range entries {
		if !e.IsDir() || e.Name() == config.RepoWorkwoodDirName {
			continue
		}
		rel := name + "/" + e.Name()
		if known[rel] {
			continue
		}
		abs := cfg.Abs(rel)
		if !gitx.IsRepo(abs) {
			continue // a stray dir that isn't a worktree → leave it alone
		}
		repo := e.Name()
		if i := strings.Index(repo, "--"); i >= 0 {
			repo = repo[:i] // strip the 2nd-worktree-of-same-repo dir suffix
		}
		if !repoSet[repo] {
			repo = "" // unknown repo: still reported, but resolution is limited
		}
		branch, _ := gitx.CurrentBranch(abs)
		base := ""
		if repo != "" {
			base = pd.DefaultBranch(repo)
		}
		res.Orphans = append(res.Orphans, Orphan{Repo: repo, Branch: branch, Base: base, Path: rel, Abs: abs})
	}
	return res, nil
}

// AdoptOrphan records an orphan worktree in the manifest (recovering it). Needs a
// known repo + a resolvable branch.
func AdoptOrphan(cfg *config.Config, name string, o Orphan) error {
	if o.Repo == "" || o.Branch == "" {
		return i18n.Err("err.adopt_unknown", o.Path)
	}
	path := cfg.ManifestPath(name)
	m, err := manifest.Load(path)
	if err != nil {
		return err
	}
	if m.Find(o.Repo, o.Branch) >= 0 {
		return nil // already tracked
	}
	m.Worktrees = append(m.Worktrees, manifest.Worktree{Repo: o.Repo, Branch: o.Branch, Base: o.Base, Path: o.Path})
	return manifest.Save(path, m)
}

// RemoveOrphan deletes an orphan worktree from disk (git worktree remove, falling
// back to a recursive delete + prune). Destructive.
func RemoveOrphan(cfg *config.Config, o Orphan) error {
	if o.Repo != "" {
		baseRepo := cfg.BaseRepo(o.Repo)
		if gitx.IsRepo(baseRepo) {
			if gitx.RemoveWorktree(baseRepo, o.Abs) == nil {
				return nil
			}
			if err := safeRemoveAll(cfg, o.Abs); err != nil {
				return err
			}
			_ = gitx.PruneWorktrees(baseRepo)
			return nil
		}
	}
	return safeRemoveAll(cfg, o.Abs)
}

// RebuildMissing re-creates a manifest worktree whose checkout is gone, pruning any
// stale registration first; like Up it attaches to the existing branch.
func RebuildMissing(cfg *config.Config, w manifest.Worktree) error {
	baseRepo := cfg.BaseRepo(w.Repo)
	if !gitx.IsRepo(baseRepo) {
		return i18n.Err("err.base_repo_missing", baseRepo)
	}
	_ = gitx.PruneWorktrees(baseRepo)
	gitx.Fetch(baseRepo)
	return addWorktree(baseRepo, w.Branch, w.Base, cfg.Abs(w.Path))
}

// DropMissing removes a worktree entry from the manifest (it has no checkout).
func DropMissing(cfg *config.Config, name string, w manifest.Worktree) error {
	path := cfg.ManifestPath(name)
	m, err := manifest.Load(path)
	if err != nil {
		return err
	}
	idx := m.Find(w.Repo, w.Branch)
	if idx < 0 {
		return nil
	}
	m.Worktrees = append(m.Worktrees[:idx], m.Worktrees[idx+1:]...)
	return manifest.Save(path, m)
}

// ReconcilePlan is a resolved set of doctor decisions: which orphan worktrees to
// adopt vs remove, and which manifest-but-missing worktrees to rebuild vs drop.
// Build it from a DiagnoseResult — the CLI from prompts/flags, the TUI from a form
// — then hand it to Reconcile. This is the DeleteWalk(plan) pattern: the caller
// resolves the decisions, the package executes them.
type ReconcilePlan struct {
	AdoptOrphans   []Orphan
	RemoveOrphans  []Orphan
	RebuildMissing []manifest.Worktree
	DropMissing    []manifest.Worktree
}

// ReconcileOutcome is one applied decision: its human message on success, or the
// Err that occurred. The front-end formats it (e.g. the TUI styles the error).
type ReconcileOutcome struct {
	Msg string
	Err error
}

// Reconcile applies a plan and returns one outcome per item, in a fixed order
// (adopt, remove, rebuild, drop). Like the doctor front-ends it replaces, it does
// NOT stop on the first error — a failure is recorded in its outcome and the
// remaining items still run.
func Reconcile(cfg *config.Config, name string, plan ReconcilePlan) []ReconcileOutcome {
	var out []ReconcileOutcome
	do := func(err error, msg string) { out = append(out, ReconcileOutcome{Msg: msg, Err: err}) }
	for _, o := range plan.AdoptOrphans {
		do(AdoptOrphan(cfg, name, o), i18n.T("doctor.adopted", o.Path))
	}
	for _, o := range plan.RemoveOrphans {
		do(RemoveOrphan(cfg, o), i18n.T("doctor.removed", o.Abs))
	}
	for _, w := range plan.RebuildMissing {
		do(RebuildMissing(cfg, w), i18n.T("doctor.rebuilt", w.Path))
	}
	for _, w := range plan.DropMissing {
		do(DropMissing(cfg, name, w), i18n.T("doctor.dropped", w.Repo, w.Branch))
	}
	return out
}
