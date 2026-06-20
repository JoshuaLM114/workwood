package update

import "testing"

func TestIsNewer(t *testing.T) {
	cases := []struct {
		latest, current string
		want            bool
	}{
		{"0.2.0", "0.1.0", true},
		{"0.1.1", "0.1.0", true},
		{"1.0.0", "0.9.9", true},
		{"0.1.0", "0.1.0", false},     // equal
		{"0.1.0", "0.2.0", false},     // older
		{"v0.2.0", "v0.1.0", true},    // leading v tolerated both sides
		{"0.2.0", "0.2.0-dev", false}, // prerelease suffix stripped → treated equal, no nag
		{"0.2", "0.1.9", true},        // missing patch → .0
		{"", "0.1.0", false},          // no release seen yet
		{"garbage", "0.1.0", false},   // unparseable → no false alarm
		{"0.2.0", "(devel)", false},   // dev build current → never nags
	}
	for _, c := range cases {
		if got := isNewer(c.latest, c.current); got != c.want {
			t.Errorf("isNewer(%q, %q) = %v, want %v", c.latest, c.current, got, c.want)
		}
	}
}
