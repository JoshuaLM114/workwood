// Package targetcfg builds and persists a super-feature's "target configuration":
// a map of editable key → absolute path that names the directories an action
// operates on.
//
// workwood is deliberately dumb about what a target means — a target is just a
// path. Each feature has a persisted WORKING SET (the enabled targets, in
// workwood-state.yml). Named PRESETS save/load the same shape to
// <data>/<project-uuid>/targets/<name>.yml. The candidate TREE shown in the TUI
// is computed live from the project's reference repos + the feature's worktrees
// (each optionally expanded into per-service sub-nodes via a committed
// .workwood/targets.yml), with any user-added arbitrary paths layered on top.
package targetcfg

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/JoshuaLM114/workwood/config"
	"github.com/JoshuaLM114/workwood/libs/fileio"
	"github.com/JoshuaLM114/workwood/libs/gitx"
	"github.com/JoshuaLM114/workwood/models"
)

// Kind classifies a candidate-tree node.
type Kind int

const (
	KindRepo          Kind = iota // a base reference clone
	KindWorktree                  // a feature worktree
	KindServiceParent             // a multi-service repo/worktree header (.workwood/targets.yml)
	KindService                   // a service under a KindServiceParent
	KindAdded                     // an enabled path that isn't auto-discovered (user-added)
)

// Node is one row in the flat candidate tree (depth + parent index encode nesting).
type Node struct {
	Key     string // working-set key (the editable name)
	Path    string // resolved absolute path
	Kind    Kind
	Depth   int
	Parent  int  // index of the parent node, -1 for top-level
	HasKids bool // a service parent with children
	Toggled bool // present in the working set
	Exists  bool // path exists on disk
}

// Toggleable reports whether a node can be enabled/disabled (everything except a
// service-parent grouping header, whose children are toggled instead).
func (n Node) Toggleable() bool { return n.Kind != KindServiceParent }

// ---- preset + working-set persistence -------------------------------------

// PresetsDir is <StateDir>/targets.
func PresetsDir(cfg *models.Config) string { return filepath.Join(cfg.StateDir, "targets") }

// PresetPath is the file for a named preset.
func PresetPath(cfg *models.Config, name string) string {
	return filepath.Join(PresetsDir(cfg), name+".yml")
}

// PresetExists reports whether a named preset file is already on disk.
func PresetExists(cfg *models.Config, name string) bool {
	_, err := os.Stat(PresetPath(cfg, name))
	return err == nil
}

// ListPresets returns the saved preset names (file stems), sorted.
func ListPresets(cfg *models.Config) []string {
	entries, err := os.ReadDir(PresetsDir(cfg))
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yml") {
			continue
		}
		out = append(out, strings.TrimSuffix(e.Name(), ".yml"))
	}
	sort.Strings(out)
	return out
}

// LoadPreset reads a named preset (key → abs path).
func LoadPreset(cfg *models.Config, name string) (models.Set, error) {
	data, err := os.ReadFile(PresetPath(cfg, name))
	if err != nil {
		return nil, err
	}
	set := models.Set{}
	if err := yaml.Unmarshal(data, &set); err != nil {
		return nil, err
	}
	return set, nil
}

// SavePreset writes a named preset, creating the targets dir as needed.
func SavePreset(cfg *models.Config, name string, set models.Set) error {
	if err := os.MkdirAll(PresetsDir(cfg), 0o755); err != nil {
		return err
	}
	data, err := yaml.Marshal(set)
	if err != nil {
		return err
	}
	return fileio.Write(PresetPath(cfg, name), data, 0o644)
}

// LoadWorking returns a copy of the feature's persisted working set.
func LoadWorking(cfg *models.Config, m *models.Manifest) (models.Set, error) {
	st, err := config.LoadState(cfg.StateFile)
	if err != nil {
		return nil, err
	}
	out := models.Set{}
	for k, v := range st.WorkingSet(m.ID) {
		out[k] = v
	}
	return out, nil
}

// Working returns the feature's effective working set: the persisted one if it
// has any entries, otherwise a freshly-seeded clean set (reference repos + the
// feature's worktrees). It does NOT persist — edits persist via SaveWorking, so a
// feature stays on the clean default until the user actually changes something.
func Working(cfg *models.Config, pd *models.ProjectDef, m *models.Manifest) (models.Set, error) {
	ws, err := LoadWorking(cfg, m)
	if err != nil {
		return nil, err
	}
	if len(ws) > 0 {
		return ws, nil
	}
	return CleanSet(cfg, pd, m), nil
}

// SaveWorking persists the feature's working set.
func SaveWorking(cfg *models.Config, m *models.Manifest, working models.Set) error {
	st, err := config.LoadState(cfg.StateFile)
	if err != nil {
		return err
	}
	st.EnsureFeature(m.ID, m.Feature)
	st.SetWorkingSet(m.ID, working)
	return config.SaveState(cfg.StateFile, st)
}

// ---- set construction + mutation ------------------------------------------

// CleanSet seeds a working set from every reference repo (key = repo name) and
// the feature's worktrees (key = sub-branch, the branch minus the "<slug>/"
// prefix). Colliding keys get a numeric suffix.
func CleanSet(cfg *models.Config, pd *models.ProjectDef, m *models.Manifest) models.Set {
	set := models.Set{}
	for _, r := range pd.Repos {
		addUnique(set, r.Name, cfg.BaseRepo(r.Name))
	}
	for _, w := range m.Worktrees {
		addUnique(set, subKey(m.Feature, w.Branch), cfg.Abs(w.Path))
	}
	return set
}

// Enable adds key→path to working (de-duping the key); a path already present
// under any key is left as-is.
func Enable(working models.Set, key, path string) {
	addUnique(working, key, path)
}

// Disable removes whatever key currently points at path.
func Disable(working models.Set, path string) {
	for k, p := range working {
		if p == path {
			delete(working, k)
			return
		}
	}
}

// Rename changes the key for an entry (de-duping the new key).
func Rename(working models.Set, oldKey, newKey string) {
	p, ok := working[oldKey]
	if !ok || newKey == oldKey || newKey == "" {
		return
	}
	delete(working, oldKey)
	addUnique(working, newKey, p)
}

// Prune drops working-set entries that live under the project's managed dirs
// (main/features) but no longer exist on disk — e.g. a worktree that was removed.
// Arbitrary external paths are kept even if absent (the user may target a dir that
// doesn't exist yet). Returns the removed keys.
func Prune(cfg *models.Config, working models.Set) []string {
	var removed []string
	for k, p := range working {
		if isManaged(cfg, p) && !pathExists(p) {
			delete(working, k)
			removed = append(removed, k)
		}
	}
	sort.Strings(removed)
	return removed
}

// Dedup returns key if unused in set, else key-2, key-3, … until free.
func Dedup(set models.Set, key string) string {
	if _, ok := set[key]; !ok {
		return key
	}
	for i := 2; ; i++ {
		k := key + "-" + strconv.Itoa(i)
		if _, ok := set[k]; !ok {
			return k
		}
	}
}

// ---- candidate tree -------------------------------------------------------

// Candidates builds the live candidate tree: reference repos + worktrees (each
// expanded into service children when it ships a .workwood/targets.yml), followed
// by any enabled working-set path that isn't auto-discovered (user-added). Each
// node's Toggled/Key reflects the current working set (matched by PATH, so a
// renamed key still shows as toggled under its working-set name).
func Candidates(cfg *models.Config, pd *models.ProjectDef, m *models.Manifest, working models.Set) []Node {
	byPath := map[string]string{} // abs path → working key
	for k, p := range working {
		byPath[p] = k
	}
	discovered := map[string]bool{}
	var nodes []Node

	add := func(defKey, path string, kind Kind, depth, parent int, hasKids bool) int {
		discovered[path] = true
		key, toggled := defKey, false
		if wk, ok := byPath[path]; ok {
			key, toggled = wk, true
		}
		nodes = append(nodes, Node{
			Key: key, Path: path, Kind: kind, Depth: depth,
			Parent: parent, HasKids: hasKids, Toggled: toggled, Exists: pathExists(path),
		})
		return len(nodes) - 1
	}

	emit := func(defKey, path string, leaf Kind) {
		services, _ := ExpandServices(path)
		if len(services) == 0 {
			add(defKey, path, leaf, 0, -1, false)
			return
		}
		parent := add(defKey, path, KindServiceParent, 0, -1, true)
		for _, name := range sortedKeys(services) {
			add(name, services[name], KindService, 1, parent, false)
		}
	}

	for _, r := range pd.Repos {
		emit(r.Name, cfg.BaseRepo(r.Name), KindRepo)
	}
	for _, w := range m.Worktrees {
		emit(subKey(m.Feature, w.Branch), cfg.Abs(w.Path), KindWorktree)
	}
	for _, k := range sortedKeys(working) {
		p := working[k]
		if discovered[p] {
			continue
		}
		nodes = append(nodes, Node{
			Key: k, Path: p, Kind: KindAdded, Depth: 0, Parent: -1, Toggled: true, Exists: pathExists(p),
		})
	}
	return nodes
}

// ExpandServices reads <rootAbs>/.workwood/targets.yml (serviceName → subpath) and
// returns serviceName → absolute path: a relative subpath is joined to rootAbs, an
// absolute one is used as-is. Missing file → (nil, nil). Parse error → (nil, err).
func ExpandServices(rootAbs string) (map[string]string, error) {
	p := filepath.Join(rootAbs, models.RepoWorkwoodDirName, "targets.yml")
	data, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var raw map[string]string
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	out := map[string]string{}
	for name, sub := range raw {
		if filepath.IsAbs(sub) {
			out[name] = sub
		} else {
			out[name] = filepath.Join(rootAbs, sub)
		}
	}
	return out, nil
}

// AddPath resolves a user-supplied path to an absolute path + a default key. If
// the path is a git repo, the default key is its current branch (needsKey=false);
// otherwise needsKey=true and the caller must prompt for a name.
func AddPath(path string) (key, abs string, needsKey bool) {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	if gitx.IsRepo(abs) {
		if b, err := gitx.CurrentBranch(abs); err == nil && b != "" && b != "HEAD" {
			return b, abs, false
		}
	}
	return "", abs, true
}

// ---- helpers --------------------------------------------------------------

func addUnique(set models.Set, key, path string) {
	for _, p := range set {
		if p == path {
			return // path already targeted under some key
		}
	}
	set[Dedup(set, key)] = path
}

func subKey(feature, branch string) string {
	if k := strings.TrimPrefix(branch, feature+"/"); k != "" {
		return k
	}
	return branch
}

func isManaged(cfg *models.Config, p string) bool {
	sep := string(os.PathSeparator)
	return strings.HasPrefix(p, cfg.MainDir+sep) || strings.HasPrefix(p, cfg.FeaturesDir+sep)
}

func pathExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
