// Package plugins embeds the default plugin scripts shipped with workwood and
// seeds them into the user's global plugin dir (~/.workwood/plugins) on first
// run. Existing files are never overwritten — a user is free to edit a seeded
// plugin, and re-seeding leaves their version alone.
package plugins

import (
	"embed"
	"io/fs"
	"os"
	"path/filepath"
)

//go:embed assets
var assets embed.FS

// Seed writes any default plugin scripts that are missing from dir, making them
// executable (0o755). It returns the names it newly wrote.
func Seed(dir string) ([]string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	entries, err := fs.ReadDir(assets, "assets")
	if err != nil {
		return nil, err
	}
	var wrote []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		dst := filepath.Join(dir, e.Name())
		if _, err := os.Stat(dst); err == nil {
			continue // never clobber a user's (possibly edited) plugin
		}
		data, err := assets.ReadFile("assets/" + e.Name())
		if err != nil {
			return wrote, err
		}
		if err := os.WriteFile(dst, data, 0o755); err != nil {
			return wrote, err
		}
		wrote = append(wrote, e.Name())
	}
	return wrote, nil
}
