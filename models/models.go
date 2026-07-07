// Package models holds workwood's core data-model type definitions and their
// (pure) methods. It is a leaf package: it imports only the standard library, so
// every other workwood package can depend on it without risking an import cycle.
//
// The functions that read, write, locate, and build these models live in their
// owning packages (config, manifest, projectdef, targetcfg, …) and operate on the
// types defined here.
package models

// File / dir name constants referenced by Config's path-helper methods. The
// remaining workwood-layout constants stay in package config.
const (
	// RepoWorkwoodDirName is the per-repo folder (in a base clone or worktree) that
	// may hold a targets.yml describing a multi-service repo's sub-paths. The same
	// folder name inside a FEATURE dir holds the back-link file (FeatureLinkName).
	RepoWorkwoodDirName = ".workwood"

	// FeatureLinkName is the back-link file workwood drops in a feature folder
	// (<FeaturesDir>/<slug>/.workwood/link.yml) so the tool can be run from there
	// and resolve back to the parent super-repo.
	FeatureLinkName = "link.yml"

	// ActionDataDirName is the folder (inside a feature's .workwood/) under which
	// each action gets its own private state directory.
	ActionDataDirName = "action-data"
)

// Set is a target configuration: editable key → absolute path.
type Set = map[string]string
