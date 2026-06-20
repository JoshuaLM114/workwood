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

import (
	"runtime/debug"
	"strings"

	"github.com/JoshuaLM114/workwood/i18n"
)

// Software is this build's version (semantic). The literal here is the
// development fallback; when installed via `go install …@vX` (or a tagged
// release build), init() below overwrites it with the real module version, so
// `workwood version` and the update check self-report correctly without
// hand-editing this file or passing -ldflags.
var Software = "0.1.0"

// Schema is the current on-disk file-format version. Bump it whenever a change
// to the manifest/state/app-settings layout would confuse an older build.
//
// v2: identity refactor — projects + super-features carry UUIDs, per-developer
// state moved into an external WORKWOOD_DATA/<project-uuid>/workwood-state.yml,
// and the global config dropped its project registry for app-settings only.
const Schema = 2

func init() {
	// go install stamps the resolved module version into the build info (e.g.
	// "v0.2.0", or a "v0.0.0-<date>-<sha>" pseudo-version for an untagged commit).
	// "(devel)" means a plain `go build` from a working tree — keep the fallback.
	if bi, ok := debug.ReadBuildInfo(); ok {
		if v := bi.Main.Version; v != "" && v != "(devel)" {
			Software = strings.TrimPrefix(v, "v")
		}
	}
}

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
