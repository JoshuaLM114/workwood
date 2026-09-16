package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"

	"github.com/JoshuaLM114/workwood/libs/fileio"
	"github.com/JoshuaLM114/workwood/manifest"
	"github.com/JoshuaLM114/workwood/models"
)

// InitResult reports what InitProject scaffolded, for the caller to print.
type InitResult struct {
	ActionsDir   string `json:"actions_dir"`
	ManifestsDir string `json:"manifests_dir"`
	StateFile    string `json:"state_file"`
	Tracked      int    `json:"tracked"` // committed features newly recorded in local state
}

// InitProject scaffolds the workwood/ tree under root, reconciles local state with
// the project identity, back-fills UUIDs on pre-UUID manifests and the feature
// back-links, and gitignores an in-repo data dir. pd is the already-loaded/created
// project def; dataDir is the resolved data dir. It performs NO interactive IO, so
// it is unit-testable end to end (the prompts stay in the caller).
func InitProject(root string, pd *models.ProjectDef, dataDir string) (*InitResult, error) {
	cfg, st, mans, err := prepareInit(root, pd, dataDir)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(cfg.ActionsDir, 0o755); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(cfg.ManifestsDir, 0o755); err != nil {
		return nil, err
	}
	st.Project = pd.ID
	if st.Name == "" {
		st.Name = projectSlug(pd, root)
	}

	// Track every committed super-feature locally, back-filling any manifest that
	// predates UUIDs (writes the committed file for the user to commit).
	tracked := 0
	for _, m := range mans {
		changed := m.ID == "" || m.Project == ""
		if m.ID == "" {
			if id, _, ok := st.FeatureBySlug(m.Feature); ok {
				m.ID = id
			} else {
				m.ID = uuid.NewString()
			}
		}
		if changed {
			m.Project = pd.ID
			if err := manifest.Save(cfg.ManifestPath(m.Feature), m); err != nil {
				return nil, err
			}
		}
		if st.EnsureFeature(m.ID, m.Feature) {
			tracked++
		}
		fs := st.Features[m.ID]
		fs.Slug = m.Feature
		st.Features[m.ID] = fs
	}

	// Backfill the feature back-link for any feature already built on disk, so
	// existing checkouts gain "run from the feature folder" without a rebuild.
	for _, m := range mans {
		if _, e := os.Stat(cfg.FeatureDir(m.Feature)); e == nil {
			if _, e := EnsureFeatureLink(cfg, m.Feature); e != nil {
				return nil, e
			}
		} else if !os.IsNotExist(e) {
			return nil, e
		}
	}

	if err := dataDirIgnored(root, dataDir); err != nil {
		return nil, err
	}
	st.SetupVersion = SetupVersion
	if err := SaveState(cfg.StateFile, st); err != nil {
		return nil, err
	}
	return &InitResult{ActionsDir: cfg.ActionsDir, ManifestsDir: cfg.ManifestsDir, StateFile: cfg.StateFile, Tracked: tracked}, nil
}

// prepareInit checks all metadata before initialization writes any of it.
func prepareInit(root string, pd *models.ProjectDef, dataDir string) (*models.Config, *models.ProjectState, []*models.Manifest, error) {
	if pd.ID == "" || dataDir == "" {
		return nil, nil, nil, fmt.Errorf("project identity and data directory are required for initialization")
	}
	cfg, err := Build(root, pd, dataDir)
	if err != nil {
		return nil, nil, nil, err
	}
	st, err := LoadState(cfg.StateFile)
	if err != nil {
		return nil, nil, nil, err
	}
	if st.SetupVersion > SetupVersion {
		return nil, nil, nil, fmt.Errorf("setup version %d requires a newer workwood (supported: %d)", st.SetupVersion, SetupVersion)
	}
	mans, err := initManifests(cfg.ManifestsDir, pd.ID)
	if err != nil {
		return nil, nil, nil, err
	}
	identities := map[string]string{}
	for _, m := range mans {
		if m.ID != "" {
			identities[m.ID] = m.Feature
		}
	}
	dirs := []string{cfg.ActionsDir, cfg.ManifestsDir}
	for _, m := range mans {
		if m.ID == "" {
			matched := false
			for id, fs := range st.Features {
				if fs.Slug != m.Feature {
					continue
				}
				if matched || id == "" || identities[id] != "" {
					return nil, nil, nil, fmt.Errorf("feature %s has ambiguous local identity; reconcile its manifest and state before initialization", m.Feature)
				}
				matched = true
			}
		}
		dirs = append(dirs, cfg.FeatureDir(m.Feature))
		if link, e := ReadFeatureLink(cfg.FeatureLinkPath(m.Feature)); e == nil && link.Version > FeatureLinkVersion {
			return nil, nil, nil, fmt.Errorf("feature link %s requires a newer workwood", cfg.FeatureLinkPath(m.Feature))
		}
	}
	for _, dir := range dirs {
		info, err := os.Stat(dir)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, nil, nil, err
		}
		if !info.IsDir() {
			return nil, nil, nil, fmt.Errorf("metadata path %s is not a directory", dir)
		}
	}
	return cfg, st, mans, nil
}

// dataDirIgnored adds the data dir to .gitignore only when it lives inside the
// super-repo (so a developer who points WORKWOOD_DATA in-repo won't commit their
// checkouts). A data dir outside the repo needs no entry.
func dataDirIgnored(root, dataDir string) error {
	rel, err := filepath.Rel(root, dataDir)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return nil
	}
	gi := filepath.Join(root, ".gitignore")
	entry := "/" + filepath.ToSlash(rel) + "/"
	data, err := os.ReadFile(gi)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == entry {
			return nil
		}
	}
	body := string(data)
	if body != "" && !strings.HasSuffix(body, "\n") {
		body += "\n"
	}
	return fileio.Write(gi, []byte(body+entry+"\n"), 0o644)
}

// projectSlug is the project's name (workwood.yml name), else the root basename.
func projectSlug(pd *models.ProjectDef, root string) string {
	if pd.Name != "" {
		return pd.Name
	}
	return filepath.Base(root)
}
