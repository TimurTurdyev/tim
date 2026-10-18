package cmd

import "testing"

func TestEditDistance(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"modt", "motd", 1}, // перестановка соседних букв - одна правка
		{"modt", "host", 2},
		{"stats", "stats", 0},
		{"statsu", "status", 1},
		{"", "go", 2},
	}
	for _, c := range cases {
		if got := editDistance(c.a, c.b); got != c.want {
			t.Errorf("%s -> %s: %d, ждали %d", c.a, c.b, got, c.want)
		}
	}
}
