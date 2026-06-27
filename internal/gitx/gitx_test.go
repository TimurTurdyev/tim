package gitx

import "testing"

func TestParseStatus(t *testing.T) {
	cases := []struct {
		in   string
		want State
	}{
		{"## main...origin/main\n", State{Branch: "main"}},
		{"## main...origin/main [ahead 2, behind 1]\n M a.go\n?? b.go\n", State{Branch: "main", Changes: 2, Ahead: 2, Behind: 1}},
		{"## feature/x...origin/feature/x [behind 3]\n", State{Branch: "feature/x", Behind: 3}},
		{"## local-only\n M a\n", State{Branch: "local-only", Changes: 1, NoUp: true}},
		{"## No commits yet on main\n", State{Branch: "main", NoUp: true}},
		{"## HEAD (no branch)\n", State{Branch: "HEAD", NoUp: true}},
	}
	for _, c := range cases {
		if got := ParseStatus(c.in); got != c.want {
			t.Errorf("%q: %+v", c.in, got)
		}
	}
}
