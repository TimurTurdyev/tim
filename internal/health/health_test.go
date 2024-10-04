package health

import (
	"testing"
	"time"
)

func TestDeadline(t *testing.T) {
	now := time.Now()
	cases := []struct {
		days int
		want Level
	}{
		{-3, Bad},
		{0, Bad},
		{5, Bad},
		{7, Bad},
		{8, Warn},
		{21, Warn},
		{22, OK},
		{200, OK},
	}
	for _, c := range cases {
		// Полсуток запаса, чтобы округление вниз не съедало ровно граничный день.
		got := Deadline("сертификат", now.Add(time.Duration(c.days)*24*time.Hour+12*time.Hour), 21, 7)
		if got.Level != c.want {
			t.Errorf("%d дн: уровень %v, ожидали %v (%s)", c.days, got.Level, c.want, got.Text)
		}
		if c.want != OK && got.Detail == "" {
			t.Errorf("%d дн: нет пояснения", c.days)
		}
	}
}

func TestRowLevelAndSort(t *testing.T) {
	rows := []Row{
		{Name: "b", Checks: []Check{{Name: "git", Level: OK}}},
		{Name: "a", Checks: []Check{{Name: "git", Level: OK}}},
		{Name: "c", Checks: []Check{{Name: "git", Level: OK}, {Name: "сайт", Level: Bad}}},
		{Name: "d", Checks: []Check{{Name: "git", Level: Skip}, {Name: "сайт", Level: Warn}}},
	}
	if l := rows[2].Level(); l != Bad {
		t.Fatalf("худшая проверка не победила: %v", l)
	}
	Sort(rows)
	var order string
	for _, r := range rows {
		order += r.Name
	}
	if order != "cdab" {
		t.Fatalf("порядок %q, ожидали \"cdab\"", order)
	}
	if ok, warn, bad := Counts(rows); ok != 2 || warn != 1 || bad != 1 {
		t.Fatalf("счёт %d/%d/%d", ok, warn, bad)
	}
}

func TestRowGetMissing(t *testing.T) {
	r := Row{Name: "x"}
	if c := r.Get("почта"); c.Level != Skip || c.Text != "-" {
		t.Fatalf("пустая колонка: %+v", c)
	}
}
