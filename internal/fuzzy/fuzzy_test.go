package fuzzy

import (
	"os"
	"slices"
	"strings"
	"testing"
)

func names(items []string, ms []Match) []string {
	out := make([]string, len(ms))
	for i, m := range ms {
		out[i] = items[m.Index]
	}
	return out
}

func TestCyrillicRunes(t *testing.T) {
	items := []string{"Проекты клиентов", "сервер"}
	ms := Filter("клиен", items)
	if len(ms) != 1 || ms[0].Index != 0 || ms[0].Positions[0] != 8 {
		t.Errorf("got %+v", ms)
	}
}

// Ранжирование на списке команд и проектов: правка весов не должна незаметно менять выдачу.
func TestRankingGolden(t *testing.T) {
	data, err := os.ReadFile("testdata/items.txt")
	if err != nil {
		t.Fatal(err)
	}
	items := strings.Split(strings.TrimSpace(string(data)), "\n")
	cases := map[string][]string{
		"ser":       {"mail-service", "tim serve <проект> -d", "tim serve stop <проект>"},
		"sd":        {"tim serve <проект> -d", "shop-frontend-v2"},
		"route st":  {"tim route status", "tim route install"},
		"disk left": {"tim disk --leftovers"},
		"mail":      {"mail-service", "tim mail <домен>"},
		"tel":       {"telecom", "telegram-bot", "lk-telecom-old"},
		"ph":        {"ProjectHere"},
		"shop fr":   {"shop-frontend-v2"},
		"pa":        {"tim proj add <папка>", "shop-api"},
		"lo":        {"tim logs <проект>", "blog", "lk-telecom-old"},
	}
	for q, want := range cases {
		got := names(items, FilterTop(q, items, 3))
		if !slices.Equal(got, want) {
			t.Errorf("%q: %q, ожидали %q", q, got, want)
		}
	}
}
