// Package update prints a non-intrusive "a newer workwood is available" notice.
//
// It is deliberately passive: workwood never modifies its own binary. The check
// hits the GitHub releases API at most once per day, caches the result under the
// workwood home, and between checks compares against that cached version with no
// network call. The notice goes to stderr (so it never corrupts piped stdout)
// and the whole thing is best-effort — any error is swallowed.
package update

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/JoshuaLM114/workwood/i18n"
)

const (
	// repoSlug is the GitHub owner/name to query releases from. It mirrors the
	// module path; the install line in the notice points at the same place.
	repoSlug = "JoshuaLM114/workwood"

	checkInterval = 24 * time.Hour
	httpTimeout   = 2 * time.Second
	cacheName     = "update.json"
)

// cache is the on-disk record under <home>/update.json.
type cache struct {
	LastCheck int64  `json:"last_check"`     // unix seconds of the last network attempt
	Latest    string `json:"latest_version"` // newest release tag seen (no leading "v")
}

// Notify prints a one-line notice to stderr when the latest release is newer
// than current. It refreshes from GitHub only when the cache is older than
// checkInterval; otherwise it reuses the cached version (no network). enabled
// gates the whole thing (config opt-out); $WORKWOOD_NO_UPDATE_CHECK forces it
// off regardless (useful in CI). All failures are silent.
func Notify(home, current string, enabled bool) {
	if !enabled || os.Getenv("WORKWOOD_NO_UPDATE_CHECK") != "" {
		return
	}
	path := filepath.Join(home, cacheName)
	c := readCache(path)

	if time.Since(time.Unix(c.LastCheck, 0)) >= checkInterval {
		// Record the attempt time either way, so a release-less or unreachable
		// repo isn't re-queried on every single invocation.
		c.LastCheck = time.Now().Unix()
		if latest, ok := fetchLatest(); ok {
			c.Latest = latest
		}
		writeCache(path, c)
	}

	if isNewer(c.Latest, current) {
		fmt.Fprintln(os.Stderr, i18n.T("update.available", c.Latest, current, repoSlug))
	}
}

// fetchLatest asks the GitHub API for the newest release tag. ok is false on any
// error, a non-200 (e.g. 404 when no releases exist yet), or an empty tag.
func fetchLatest() (latest string, ok bool) {
	ctx, cancel := context.WithTimeout(context.Background(), httpTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"https://api.github.com/repos/"+repoSlug+"/releases/latest", nil)
	if err != nil {
		return "", false
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", false
	}
	var body struct {
		Tag string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", false
	}
	tag := strings.TrimPrefix(strings.TrimSpace(body.Tag), "v")
	return tag, tag != ""
}

// isNewer reports whether semantic version latest is strictly greater than
// current. Either side failing to parse yields false, so a malformed tag or a
// dev build never produces a false "update available" alarm.
func isNewer(latest, current string) bool {
	lv, ok1 := parseSemver(latest)
	cv, ok2 := parseSemver(current)
	if !ok1 || !ok2 {
		return false
	}
	for i := range 3 {
		if lv[i] != cv[i] {
			return lv[i] > cv[i]
		}
	}
	return false
}

// parseSemver extracts major.minor.patch, ignoring any -prerelease/+build suffix
// and a leading "v". Missing trailing components default to 0 (so "1.2" is 1.2.0).
func parseSemver(s string) (v [3]int, ok bool) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "v")
	if i := strings.IndexAny(s, "-+"); i >= 0 {
		s = s[:i]
	}
	if s == "" {
		return v, false
	}
	parts := strings.Split(s, ".")
	if len(parts) > 3 {
		return v, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return v, false
		}
		v[i] = n
	}
	return v, true
}

func readCache(path string) cache {
	var c cache
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &c)
	}
	return c
}

func writeCache(path string, c cache) {
	data, err := json.Marshal(c)
	if err != nil {
		return
	}
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	_ = os.WriteFile(path, data, 0o644)
}
