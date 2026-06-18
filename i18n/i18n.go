// Package i18n is workwood's tiny message catalog. Every user-facing English
// string lives in locales/en.json keyed by an ID; translations live alongside
// (locales/ja.json). The catalogs are embedded, so the binary stays
// self-contained, but the JSON files are the single source of the wording.
//
// Use T(key, args...) for plain text, Err/Errw for errors. A missing key falls
// back to English, then to the key itself (so gaps are visible, never fatal).
package i18n

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
)

//go:embed locales/*.json
var locales embed.FS

// Supported lists the known languages, in display order. The first is the base
// language that every other falls back to.
var Supported = []string{"en", "ja"}

var (
	active   = "en"
	catalogs = map[string]map[string]string{}
)

func load() {
	if len(catalogs) > 0 {
		return
	}
	for _, lang := range Supported {
		data, err := locales.ReadFile("locales/" + lang + ".json")
		if err != nil {
			continue
		}
		m := map[string]string{}
		if json.Unmarshal(data, &m) == nil {
			catalogs[lang] = m
		}
	}
}

// IsSupported reports whether lang is a known catalog code.
func IsSupported(lang string) bool {
	for _, l := range Supported {
		if l == lang {
			return true
		}
	}
	return false
}

// Resolve picks the active language by precedence: $WORKWOOD_LANG → configLang →
// $LC_ALL/$LC_MESSAGES/$LANG → "en". The result is always a supported code.
func Resolve(configLang string) string {
	for _, cand := range []string{os.Getenv("WORKWOOD_LANG"), configLang, envLang()} {
		if c := normalize(cand); IsSupported(c) {
			return c
		}
	}
	return "en"
}

func envLang() string {
	for _, k := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if v := os.Getenv(k); v != "" {
			return v
		}
	}
	return ""
}

// normalize reduces a locale like "ja_JP.UTF-8" or "JA" to "ja".
func normalize(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	for _, sep := range []string{".", "_", "-"} {
		if i := strings.Index(s, sep); i >= 0 {
			s = s[:i]
		}
	}
	return s
}

// Init loads the catalogs and sets the active language (unsupported → "en").
func Init(lang string) {
	load()
	if IsSupported(lang) {
		active = lang
	} else {
		active = "en"
	}
}

// Lang returns the active language code.
func Lang() string { return active }

// T returns key's message in the active language (falling back to English, then
// the key), formatted with args.
func T(key string, args ...any) string {
	format := lookup(key)
	if len(args) == 0 {
		return format
	}
	return fmt.Sprintf(format, args...)
}

func lookup(key string) string {
	if m, ok := catalogs[active]; ok {
		if v, ok := m[key]; ok {
			return v
		}
	}
	if m, ok := catalogs["en"]; ok {
		if v, ok := m[key]; ok {
			return v
		}
	}
	return key
}

// Err returns a new error whose message is T(key, args...).
func Err(key string, args ...any) error { return errors.New(T(key, args...)) }

// Errw wraps err, prefixing it with the translated message T(key, args...).
func Errw(err error, key string, args ...any) error {
	return fmt.Errorf("%s: %w", T(key, args...), err)
}
