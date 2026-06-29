package config

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/JoshuaLM114/workwood/fileio"
	"github.com/JoshuaLM114/workwood/i18n"
	"github.com/JoshuaLM114/workwood/manifest"
	"github.com/JoshuaLM114/workwood/projectdef"
	"github.com/google/uuid"
)

// InitResult reports what InitProject scaffolded, for the caller to print.
type InitResult struct {
	ActionsDir   string
	ManifestsDir string
	StateFile    string
	Tracked      int // committed features newly recorded in local state
}

// InitProject scaffolds the workwood/ tree under root, reconciles local state with
// the project identity, back-fills UUIDs on pre-UUID manifests and the feature
// back-links, and gitignores an in-repo data dir. pd is the already-loaded/created
// project def; dataDir is the resolved data dir. It performs NO interactive IO, so
// it is unit-testable end to end (the prompts stay in the caller).
func InitProject(root string, pd *projectdef.File, dataDir string) (*InitResult, error) {
	actionsDir := filepath.Join(root, WorkwoodDirName, ActionsDirName)
	manifestsDir := filepath.Join(root, WorkwoodDirName, ManifestsDirName)
	if err := os.MkdirAll(actionsDir, 0o755); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(manifestsDir, 0o755); err != nil {
		return nil, err
	}

	stateFile := filepath.Join(dataDir, StateFileName)
	st, err := LoadState(stateFile)
	if err != nil {
		return nil, err
	}
	if st.Project != "" && st.Project != pd.ID {
		return nil, i18n.Err("err.project_identity_mismatch", stateFile, st.Project, pd.ID)
	}
	st.Project = pd.ID
	if st.Name == "" {
		st.Name = projectSlug(pd, root)
	}

	// Track every committed super-feature locally, back-filling any manifest that
	// predates UUIDs (writes the committed file for the user to commit).
	mans, err := manifest.List(manifestsDir)
	if err != nil {
		return nil, err
	}
	tracked := 0
	for _, m := range mans {
		if m.ID == "" {
			m.ID = uuid.NewString()
			m.Project = pd.ID
			if err := manifest.Save(filepath.Join(manifestsDir, m.Feature+".yaml"), m); err != nil {
				return nil, err
			}
		}
		if st.EnsureFeature(m.ID, m.Feature) {
			tracked++
		}
	}
	if err := SaveState(stateFile, st); err != nil {
		return nil, err
	}

	// Backfill the feature back-link for any feature already built on disk, so
	// existing checkouts gain "run from the feature folder" without a rebuild.
	if cfg, err := Build(root, pd, dataDir); err == nil {
		for _, m := range mans {
			if _, e := os.Stat(cfg.FeatureDir(m.Feature)); e == nil {
				if e := WriteFeatureLink(cfg, m.Feature); e != nil {
					return nil, e
				}
			}
		}
	}

	if err := dataDirIgnored(root, dataDir); err != nil {
		return nil, err
	}
	return &InitResult{ActionsDir: actionsDir, ManifestsDir: manifestsDir, StateFile: stateFile, Tracked: tracked}, nil
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
func projectSlug(pd *projectdef.File, root string) string {
	if pd.Name != "" {
		return pd.Name
	}
	return filepath.Base(root)
}
