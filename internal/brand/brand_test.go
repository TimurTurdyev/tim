package brand

import (
	"strings"
	"testing"
)

func TestDetectDepth(t *testing.T) {
	cases := []struct {
		ct, term string
		want     Depth
	}{
		{"truecolor", "xterm-256color", TrueColor},
		{"24bit", "", TrueColor},
		{"", "xterm-256color", Color256}, // Apple Terminal
		{"", "xterm", Basic},
		{"", "dumb", Basic},
	}
	for _, c := range cases {
		if got := DetectDepth(c.ct, c.term); got != c.want {
			t.Errorf("%q %q: %v", c.ct, c.term, got)
		}
	}
}

func TestCode256(t *testing.T) {
	// Чистые цвета куба попадают в свои ячейки: чёрный 16, белый 231.
	if (RGB{0, 0, 0}).Code256() != 16 || (RGB{255, 255, 255}).Code256() != 231 {
		t.Fatal("углы куба")
	}
	// Бирюзовый остаётся сине-зелёным, а не уезжает в серый.
	c := Teal.Code256() - 16
	r, g, b := c/36, c/6%6, c%6
	if g <= r || b <= r {
		t.Fatalf("бирюзовый в 256: %d (%d %d %d)", Teal.Code256(), r, g, b)
	}
}

func TestMascot(t *testing.T) {
	if Mascot(Calm, Basic) != nil {
		t.Fatal("без 256 цветов маскот не рисуется")
	}
	for _, m := range []Mood{Calm, Happy, Upset, Asleep} {
		lines := Mascot(m, TrueColor)
		if len(lines) != 6 {
			t.Fatalf("%v: строк %d", m, len(lines))
		}
		for _, l := range lines {
			plain := stripANSI(l)
			if n := len([]rune(plain)); n != MascotWidth {
				t.Fatalf("%v: ширина %d в %q", m, n, plain)
			}
		}
	}
}

func stripANSI(s string) string {
	var b strings.Builder
	in := false
	for _, r := range s {
		switch {
		case r == '\x1b':
			in = true
		case in && r == 'm':
			in = false
		case !in:
			b.WriteRune(r)
		}
	}
	return b.String()
}
