package config

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"github.com/JoshuaLM114/workwood/i18n"
	"github.com/JoshuaLM114/workwood/libs/fileio"
	"github.com/JoshuaLM114/workwood/models"
	"github.com/JoshuaLM114/workwood/version"
)

// LoadState reads a workwood-state.yml. A missing file yields empty state (not an
// error), so an un-initialised project still resolves to sensible defaults.
func LoadState(path string) (*models.ProjectState, error) {
	st := &models.ProjectState{Features: map[string]models.FeatureState{}}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return st, nil
		}
		return nil, err
	}
	if err := yaml.Unmarshal(data, st); err != nil {
		return nil, i18n.Errw(err, "err.parse_file", path)
	}
	if err := version.CheckSchema(st.Version, i18n.T("noun.state")+" "+path); err != nil {
		return nil, err
	}
	if st.Features == nil {
		st.Features = map[string]models.FeatureState{}
	}
	return st, nil
}

// SaveState writes a workwood-state.yml, stamping the current schema and creating
// the data dir as needed.
func SaveState(path string, st *models.ProjectState) error {
	st.Version = version.Schema
	if st.Features == nil {
		st.Features = map[string]models.FeatureState{}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return fileio.WriteYAML(path, st)
}
