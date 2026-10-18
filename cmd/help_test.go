package cmd

import (
	"strings"
	"testing"
)

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

func TestRenamedHint(t *testing.T) {
	cases := map[string]string{
		"vpn exclude example.org": "tim direct",
		"ssl":                     "tim cert",
		"route clear":             "tim direct rm",
		"route":                   "",
		"vpn excluded":            "",
	}
	for typed, want := range cases {
		got := renamedHint(strings.Fields(typed))
		if want == "" && got != "" || want != "" && !strings.Contains(got, "теперь "+want) {
			t.Errorf("%q: %q", typed, got)
		}
	}
}
