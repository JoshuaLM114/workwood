// Package version centralises the workwood build version and the on-disk
// file-format (schema) version, plus the compatibility check applied when
// loading saved files.
//
// Every file workwood writes (manifests, target state, the registry) carries
// the schema version it was written with. When a file is read, a schema newer
// than this build understands is rejected rather than silently misread — so a
// teammate on an older binary gets a clear "upgrade" message instead of corrupt
// behaviour. Missing/older schemas are accepted and re-stamped on the next save
// (forward migration).
package version

import "github.com/JoshuaLM114/workwood/i18n"

const (
	// Software is this build's version (semantic). Bumped on releases.
	Software = "0.1.0"

	// Schema is the current on-disk file-format version. Bump it whenever a
	// change to the manifest/state/registry layout would confuse an older build.
	Schema = 1
)

// CheckSchema returns an error when fileSchema is newer than this build's Schema.
// what names the file for the message (e.g. "manifest foo.yaml").
func CheckSchema(fileSchema int, what string) error {
	if fileSchema > Schema {
		return i18n.Err("err.schema_too_new", what, fileSchema, Schema, Software)
	}
	return nil
}

// Normalize maps a missing/zero schema to the baseline 1, so legacy files (which
// predate the version field) are treated as the original format.
func Normalize(fileSchema int) int {
	if fileSchema <= 0 {
		return 1
	}
	return fileSchema
}
