// Package fileio provides crash-safe file writes. Data is written to a temp file
// in the destination directory and atomically renamed over the target, so a crash,
// a full disk, or an interrupted write can never leave a half-written (corrupt)
// file in place. That matters for the files workwood reads back and trusts — the
// committed super-feature manifests and the local workwood-state.yml.
package fileio

import (
	"bytes"
	"os"

	"github.com/google/renameio/v2"
	"gopkg.in/yaml.v3"
)

// Write atomically replaces path with data (temp file in the same dir + rename).
func Write(path string, data []byte, perm os.FileMode) error {
	return renameio.WriteFile(path, data, perm)
}

// WriteYAML atomically writes v as 2-space-indented YAML to path. Unlike a bare
// yaml.Marshal + WriteFile it also surfaces the encoder Close error — a final-flush
// failure that would otherwise let a truncated document be persisted as success.
func WriteYAML(path string, v any) error {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(v); err != nil {
		return err
	}
	if err := enc.Close(); err != nil {
		return err
	}
	return Write(path, buf.Bytes(), 0o644)
}
