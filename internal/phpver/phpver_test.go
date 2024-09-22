package phpver

import "testing"

func v(s string) Version { x, _ := Parse(s); return x }

func TestSatisfies(t *testing.T) {
	cases := []struct {
		ver, c string
		want   bool
	}{
		{"8.3", "^8.2", true},
		{"8.1", "^8.2", false},
		{"9.0", "^8.2", false},
		{"8.4", "~8.2", true},
		{"8.3", "~8.2.0", false},
		{"8.2.5", "~8.2.0", true},
		{"8.1", ">=7.4", true},
		{"8.1", ">=8.0 <8.2", true},
		{"8.2", ">=8.0 <8.2", false},
		{"8.1", ">=8.0,<8.2", true},
		{"7.4", "^7.4 || ^8.0", true},
		{"8.5", "^7.4 || ^8.0", true},
		{"8.3", "8.3.*", true},
		{"8.4", "8.3.*", false},
		{"8.4", "8.*", true},
		{"8.3", "*", true},
		{"8.3", "8.3", true},
		{"8.3", "", false},
	}
	for _, c := range cases {
		if got := Satisfies(v(c.ver), c.c); got != c.want {
			t.Errorf("%s vs %q: %v", c.ver, c.c, got)
		}
	}
}
