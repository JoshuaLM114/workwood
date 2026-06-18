package superfeature

import "testing"

func TestResolveBranchWith(t *testing.T) {
	cases := []struct {
		name    string
		feature string
		sub     string
		omit    bool
		want    string
	}{
		{"simple", "voice", "api", false, "voice/api"},
		{"already-prefixed", "voice", "voice/api", false, "voice/api"},
		{"slashed-sub", "voice", "fix/login", false, "voice/fix/login"},
		{"omit-prefix", "voice", "hotfix", true, "hotfix"},
		{"sub-equals-feature", "voice", "voice", false, "voice"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ResolveBranchWith(c.feature, c.sub, c.omit); got != c.want {
				t.Fatalf("ResolveBranchWith(%q,%q,%v) = %q, want %q", c.feature, c.sub, c.omit, got, c.want)
			}
		})
	}
}
