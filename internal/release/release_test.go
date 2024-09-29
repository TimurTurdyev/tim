package release

import (
	"strings"
	"testing"
)

func TestParseChecksums(t *testing.T) {
	sum := strings.Repeat("a", 64)
	m := ParseChecksums(strings.NewReader(sum + "  tim_1.2.0_darwin_all.tar.gz\nbroken line\n" + strings.Repeat("B", 64) + " *x.tar.gz\n"))
	if m["tim_1.2.0_darwin_all.tar.gz"] != sum || m["x.tar.gz"] != strings.Repeat("b", 64) || len(m) != 2 {
		t.Errorf("%v", m)
	}
}

func TestNewer(t *testing.T) {
	cases := []struct {
		l, c string
		want bool
	}{
		{"v1.2.0", "v1.1.9", true},
		{"v1.2.0", "1.2.0", false},
		{"v1.10.0", "v1.9.3", true},
		{"v1.2.0", "dev", true},
		{"v1.2.0", "0b4497d", true},
		{"v1.2.0", "v1.2.1-3-gabc-dirty", false},
	}
	for _, c := range cases {
		if got := Newer(c.l, c.c); got != c.want {
			t.Errorf("%s vs %s: %v", c.l, c.c, got)
		}
	}
}
