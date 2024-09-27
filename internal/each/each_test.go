package each

import "testing"

func TestOlder(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"2.18.3", "2.19.0", true},
		{"2.19.0", "2.19.0", false},
		{"2.19.1", "2.19.0", false},
		{"2.9", "2.10.0", true},
		{"v2.19.0-beta.1", "2.19.0", false},
		{"tool version 1.4.2 (darwin)", "1.5", true},
		{"", "2.19.0", true},
	}
	for _, c := range cases {
		if got := Older(c.a, c.b); got != c.want {
			t.Errorf("%s < %s: %v", c.a, c.b, got)
		}
	}
}
