package i18n

import "testing"

// clearEnv removes the locale env vars so Resolve is deterministic under test.
func clearEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{"WORKWOOD_LANG", "LC_ALL", "LC_MESSAGES", "LANG"} {
		t.Setenv(k, "")
	}
}

func TestResolvePrecedence(t *testing.T) {
	cases := []struct {
		name      string
		wlang     string
		lang      string
		configArg string
		want      string
	}{
		{"default-en", "", "", "", "en"},
		{"config-ja", "", "", "ja", "ja"},
		{"env-wins-over-config", "en", "", "ja", "en"},
		{"lang-autodetect", "", "ja_JP.UTF-8", "", "ja"},
		{"unsupported-falls-back", "", "", "fr", "en"},
		{"wlang-locale-form", "ja-JP", "", "", "ja"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			clearEnv(t)
			if c.wlang != "" {
				t.Setenv("WORKWOOD_LANG", c.wlang)
			}
			if c.lang != "" {
				t.Setenv("LANG", c.lang)
			}
			if got := Resolve(c.configArg); got != c.want {
				t.Fatalf("Resolve(%q) [WORKWOOD_LANG=%q LANG=%q] = %q, want %q", c.configArg, c.wlang, c.lang, got, c.want)
			}
		})
	}
}

func TestTFallback(t *testing.T) {
	Init("ja")
	if got := T("cli.error_prefix"); got != "エラー:" {
		t.Errorf("ja lookup = %q, want エラー:", got)
	}
	// A key absent from every catalog returns the key itself.
	if got := T("nope.not_a_key"); got != "nope.not_a_key" {
		t.Errorf("missing key = %q, want the key back", got)
	}
	// Formatting with args works.
	if got := T("feature.added", "a", "b", "c"); got == "feature.added" {
		t.Errorf("expected a formatted message, got the raw key")
	}
}

func TestCatalogsParity(t *testing.T) {
	load()
	en, ja := catalogs["en"], catalogs["ja"]
	if len(en) == 0 || len(ja) == 0 {
		t.Fatal("catalogs failed to load")
	}
	for k := range en {
		if _, ok := ja[k]; !ok {
			t.Errorf("ja.json missing key %q", k)
		}
	}
	for k := range ja {
		if _, ok := en[k]; !ok {
			t.Errorf("ja.json has key %q not in en.json", k)
		}
	}
}
