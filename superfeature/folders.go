package superfeature

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/JoshuaLM114/workwood/config"
	"github.com/JoshuaLM114/workwood/i18n"
	"github.com/JoshuaLM114/workwood/libs/fileio"
	"github.com/JoshuaLM114/workwood/libs/gitx"
	"github.com/JoshuaLM114/workwood/manifest"
	"github.com/JoshuaLM114/workwood/models"
	"github.com/JoshuaLM114/workwood/targetcfg"
	"github.com/JoshuaLM114/workwood/version"
)

// FolderChange identifies a recorded folder that needs the repo--branch name.
// Path is the proposed replacement, relative to FeaturesDir.
type FolderChange struct {
	Worktree models.Worktree `json:"worktree"`
	Path     string          `json:"path"`
	Missing  bool            `json:"missing"`
}

// FolderDecision renames a folder, or drops only its manifest entry when false.
type FolderDecision struct {
	FolderChange
	Rename bool `json:"rename"`
}

// CheckFolderNames proposes collision-free names without changing any files.
// Existing numeric collision suffixes remain valid even after a sibling is gone.
func CheckFolderNames(cfg *models.Config, name string) ([]FolderChange, error) {
	if err := validName(name); err != nil {
		return nil, err
	}
	m, err := manifest.Load(cfg.ManifestPath(name))
	if err != nil {
		return nil, err
	}
	if err := checkProject(cfg, m); err != nil {
		return nil, err
	}
	if m.Feature != name {
		return nil, i18n.Err("err.folder_feature_mismatch", m.Feature, name)
	}
	var changes []FolderChange
	reserved := *m
	reserved.Worktrees = append([]models.Worktree(nil), m.Worktrees...)
	for _, w := range m.Worktrees {
		if err := validName(w.Repo); err != nil {
			return nil, err
		}
		base := worktreePath(m, w.Repo, w.Branch)
		if w.Path == base {
			continue
		}
		if suffix, ok := strings.CutPrefix(w.Path, base+"--"); ok {
			if n, err := strconv.Atoi(suffix); err == nil && n >= 2 && strconv.Itoa(n) == suffix {
				continue
			}
		}
		_, err := os.Lstat(cfg.Abs(w.Path))
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		path := allocPath(cfg, &reserved, w.Repo, w.Branch)
		changes = append(changes, FolderChange{Worktree: w, Path: path, Missing: errors.Is(err, os.ErrNotExist)})
		reserved.Worktrees = append(reserved.Worktrees, models.Worktree{Path: path})
	}
	return changes, nil
}

// ApplyFolderNames applies a complete, reviewed set of folder decisions. Git
// moves preserve checkout contents; dropping an entry leaves all local files,
// branches and independently configured action targets intact. Renamed paths
// propagate into saved working sets and presets, including service subpaths.
// A failed move or write rolls back earlier moves and writes; rollback failures
// are returned with the affected paths so they can be repaired explicitly.
func ApplyFolderNames(cfg *models.Config, name string, decisions []FolderDecision) ([]string, error) {
	current, err := CheckFolderNames(cfg, name)
	if err != nil {
		return nil, err
	}
	if len(current) != len(decisions) {
		return nil, i18n.Err("err.folder_plan_changed")
	}
	for i, decision := range decisions {
		if decision.FolderChange != current[i] {
			return nil, i18n.Err("err.folder_plan_changed")
		}
	}
	if len(decisions) == 0 {
		return nil, nil
	}
	m, err := manifest.Load(cfg.ManifestPath(name))
	if err != nil {
		return nil, err
	}
	var moves []FolderChange
	renamed := map[string]string{}
	var log []string
	for _, decision := range decisions {
		w := decision.Worktree
		i := m.Find(w.Repo, w.Branch)
		if i < 0 || m.Worktrees[i] != w {
			return nil, i18n.Err("err.folder_plan_changed")
		}
		if !decision.Rename {
			m.Worktrees = append(m.Worktrees[:i], m.Worktrees[i+1:]...)
			log = append(log, i18n.T("folders.dropped", w.Repo, w.Branch, cfg.Abs(w.Path)))
			continue
		}
		// A legacy folder is a direct child of this feature, never a symlink or
		// a path that can resolve into a different feature or the base clones.
		if filepath.ToSlash(filepath.Clean(w.Path)) != w.Path ||
			filepath.ToSlash(filepath.Dir(w.Path)) != name || filepath.Base(w.Path) == models.RepoWorkwoodDirName {
			return nil, i18n.Err("err.folder_unsafe", w.Path)
		}
		parent := cfg.FeatureDir(name)
		if info, err := os.Lstat(parent); err == nil {
			if !info.IsDir() {
				return nil, i18n.Err("err.folder_unsafe", parent)
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		oldPath, newPath := cfg.Abs(w.Path), cfg.Abs(decision.Path)
		if _, err := os.Lstat(newPath); !errors.Is(err, os.ErrNotExist) {
			if err != nil {
				return nil, err
			}
			return nil, i18n.Err("err.folder_plan_changed")
		}
		if !decision.Missing {
			info, err := os.Lstat(oldPath)
			if err != nil {
				return nil, err
			}
			if !info.IsDir() || !gitx.IsRepo(oldPath) {
				return nil, i18n.Err("err.folder_unsafe", oldPath)
			}
			branch, err := gitx.CurrentBranch(oldPath)
			if err != nil {
				return nil, err
			}
			if branch != w.Branch {
				return nil, i18n.Err("err.folder_branch_mismatch", oldPath, branch, w.Branch)
			}
			moves = append(moves, decision.FolderChange)
		}
		m.Worktrees[i].Path = decision.Path
		renamed[oldPath] = newPath
		log = append(log, i18n.T("folders.renamed", oldPath, newPath))
	}

	// Prepare every metadata write before moving checkouts. Originals retain
	// their exact bytes for rollback, including comments and formatting.
	type fileChange struct {
		path          string
		before, after []byte
		mode          os.FileMode
	}
	var files []fileChange
	prepare := func(path string, value any) error {
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return i18n.Err("err.folder_unsafe", path)
		}
		before, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var buf bytes.Buffer
		enc := yaml.NewEncoder(&buf)
		enc.SetIndent(2)
		if err := enc.Encode(value); err != nil {
			return err
		}
		if err := enc.Close(); err != nil {
			return err
		}
		files = append(files, fileChange{path: path, before: before, after: buf.Bytes(), mode: info.Mode().Perm()})
		return nil
	}
	remap := func(set map[string]string) bool {
		changed := false
		for key, path := range set {
			for oldPath, newPath := range renamed {
				rel, err := filepath.Rel(oldPath, path)
				if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
					set[key] = filepath.Join(newPath, rel)
					changed = true
					break
				}
			}
		}
		return changed
	}
	if len(renamed) > 0 && cfg.StateFile != "" {
		st, err := config.LoadState(cfg.StateFile)
		if err != nil {
			return nil, err
		}
		changed := false
		for _, fs := range st.Features {
			if remap(fs.Targets) {
				changed = true
			}
		}
		if changed {
			if err := prepare(cfg.StateFile, st); err != nil {
				return nil, err
			}
		}
	}
	if len(renamed) > 0 && cfg.StateDir != "" {
		entries, err := os.ReadDir(targetcfg.PresetsDir(cfg))
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yml") {
				continue
			}
			preset := strings.TrimSuffix(entry.Name(), ".yml")
			set, err := targetcfg.LoadPreset(cfg, preset)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", targetcfg.PresetPath(cfg, preset), err)
			}
			if remap(set) {
				if err := prepare(targetcfg.PresetPath(cfg, preset), set); err != nil {
					return nil, err
				}
			}
		}
	}
	m.Version = version.Schema
	if err := prepare(cfg.ManifestPath(name), m); err != nil {
		return nil, err
	}

	moved, written := 0, 0
	rollback := func(cause error) ([]string, error) {
		for i := written - 1; i >= 0; i-- {
			f := files[i]
			if err := fileio.Write(f.path, f.before, f.mode); err != nil {
				cause = errors.Join(cause, i18n.Errw(err, "err.folder_restore_file", f.path))
			}
		}
		for i := moved - 1; i >= 0; i-- {
			move := moves[i]
			if err := gitx.MoveWorktree(cfg.BaseRepo(move.Worktree.Repo), cfg.Abs(move.Path), cfg.Abs(move.Worktree.Path)); err != nil {
				cause = errors.Join(cause, i18n.Errw(err, "err.folder_restore_move", cfg.Abs(move.Path), cfg.Abs(move.Worktree.Path)))
			}
		}
		return nil, cause
	}
	for _, move := range moves {
		if err := gitx.MoveWorktree(cfg.BaseRepo(move.Worktree.Repo), cfg.Abs(move.Worktree.Path), cfg.Abs(move.Path)); err != nil {
			return rollback(err)
		}
		moved++
	}
	for _, f := range files {
		if err := fileio.Write(f.path, f.after, f.mode); err != nil {
			return rollback(err)
		}
		written++
	}
	return log, nil
}
