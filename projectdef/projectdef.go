// Package projectdef reads (and scaffolds) a project's workwood.yml — the base
// repos that make up a super-project and each repo's default branch. It is the
// committed, team-shared definition that lives at the root of a super-repo. The
// parsed shape (models.ProjectDef / models.Repo) lives in package models; this
// package holds the read/write functions over it.
package projectdef

import (
	"os"

	"gopkg.in/yaml.v3"

	"github.com/JoshuaLM114/workwood/i18n"
	"github.com/JoshuaLM114/workwood/libs/fileio"
	"github.com/JoshuaLM114/workwood/models"
)

// Load parses the workwood.yml at path.
func Load(path string) (*models.ProjectDef, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, i18n.Err("err.no_project_def", path)
		}
		return nil, err
	}
	var f models.ProjectDef
	if err := yaml.Unmarshal(data, &f); err != nil {
		return nil, i18n.Errw(err, "err.parse_file", path)
	}
	return &f, nil
}

// Save writes a workwood.yml to path (used by `workwood init` when scaffolding a
// fresh super-repo).
func Save(path string, f *models.ProjectDef) error {
	return fileio.WriteYAML(path, f)
}
