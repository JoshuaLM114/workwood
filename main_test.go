package main

import (
	"bufio"
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/JoshuaLM114/workwood/i18n"
	"github.com/JoshuaLM114/workwood/superfeature"
)

func eqStrs(t *testing.T, got, want []string, ctx string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s = %v, want %v", ctx, got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("%s[%d] = %q, want %q", ctx, i, got[i], want[i])
		}
	}
}

func TestSplitFlags(t *testing.T) {
	pos, flags := splitFlags([]string{"feat", "repo", "--from", "main", "--base-source", "pull", "--no-feature-prefix", "--name=x", "extra"})
	eqStrs(t, pos, []string{"feat", "repo", "extra"}, "pos")

	// A value flag in the allowlist consumes the next arg…
	if flags["from"] != "main" {
		t.Errorf("--from = %q, want main", flags["from"])
	}
	if flags["base-source"] != "pull" {
		t.Errorf("--base-source = %q, want pull", flags["base-source"])
	}
	// …a "=" form binds inline…
	if flags["name"] != "x" {
		t.Errorf("--name=x = %q, want x", flags["name"])
	}
	// …and a boolean (non-allowlist) flag is empty and does NOT eat the next arg.
	if v, ok := flags["no-feature-prefix"]; !ok || v != "" {
		t.Errorf("--no-feature-prefix = (%q,%v), want (\"\",true)", v, ok)
	}
}

func TestPromptBaseSource(t *testing.T) {
	i18n.Init("en")
	status := superfeature.BaseStatus{Base: "main", LocalExists: true, OriginExists: true, LocalBehind: 2}
	for _, tc := range []struct {
		name, input, want string
	}{
		{"origin number", "1\n", superfeature.BaseSourceOrigin},
		{"local name", "local\n", superfeature.BaseSourceLocal},
		{"pull number", "3\n", superfeature.BaseSourcePull},
		{"default", "\n", superfeature.BaseSourceOrigin},
		{"retry invalid", "x\n2\n", superfeature.BaseSourceLocal},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			got, err := promptBaseSource(bufio.NewScanner(strings.NewReader(tc.input)), &out, status)
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
			require.Contains(t, out.String(), "2 behind")
		})
	}
}

func TestSplitFlagsBooleanKeepsFollowingPositional(t *testing.T) {
	pos, flags := splitFlags([]string{"--init", "myfeature"})
	if _, ok := flags["init"]; !ok {
		t.Error("--init not parsed")
	}
	eqStrs(t, pos, []string{"myfeature"}, "pos")
}

func TestExtractProjectFlag(t *testing.T) {
	cases := []struct {
		args        []string
		wantProject string
		wantRest    []string
	}{
		{[]string{"-p", "/x", "cmd"}, "/x", []string{"cmd"}},
		{[]string{"--project", "/y", "cmd"}, "/y", []string{"cmd"}},
		{[]string{"--project=/z", "cmd"}, "/z", []string{"cmd"}},
		{[]string{"-p=/w", "cmd"}, "/w", []string{"cmd"}},
		{[]string{"cmd", "arg"}, "", []string{"cmd", "arg"}},
	}
	for _, c := range cases {
		gotP, gotRest := extractProjectFlag(c.args)
		if gotP != c.wantProject {
			t.Errorf("extractProjectFlag(%v) project = %q, want %q", c.args, gotP, c.wantProject)
		}
		eqStrs(t, gotRest, c.wantRest, "rest")
	}
}

func TestSmallHelpers(t *testing.T) {
	if !hasFlag(map[string]string{"adopt": ""}, "rebuild", "adopt") {
		t.Error("hasFlag should match adopt")
	}
	if hasFlag(map[string]string{}, "x") {
		t.Error("hasFlag on empty map = true, want false")
	}
	if firstPos(nil) != "" || firstPos([]string{"a", "b"}) != "a" {
		t.Error("firstPos wrong")
	}
	if firstNonEmpty("", "", "z") != "z" || firstNonEmpty("") != "" {
		t.Error("firstNonEmpty wrong")
	}
	if nz("") != "?" || nz("x") != "x" {
		t.Error("nz wrong")
	}
}
