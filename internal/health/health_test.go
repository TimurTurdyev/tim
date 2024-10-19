package health

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/TimurTurdyev/tim/internal/sysinfo"
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

func TestSiteCheck(t *testing.T) {
	cases := []struct {
		status int
		err    error
		want   Level
	}{
		{200, nil, OK},
		{301, nil, OK},
		{404, nil, Warn},
		{403, nil, Warn},
		{500, nil, Bad},
		{503, nil, Bad},
		{0, errors.New("dial tcp: i/o timeout"), Bad},
	}
	for _, c := range cases {
		got := SiteCheck(c.status, c.err)
		if got.Level != c.want {
			t.Errorf("%d %v -> %v, ожидали %v", c.status, c.err, got.Level, c.want)
		}
		if c.want != OK && got.Detail == "" {
			t.Errorf("%d: нет пояснения", c.status)
		}
	}
	// Из обёрнутой ошибки в пояснение попадает только последняя часть.
	if d := SiteCheck(0, errors.New("get https://x: dial tcp: нет маршрута")).Detail; !strings.HasSuffix(d, "нет маршрута") {
		t.Errorf("пояснение: %q", d)
	}
}

func TestGitCheck(t *testing.T) {
	cases := []struct {
		g    sysinfo.GitState
		want Level
		text string
	}{
		{sysinfo.GitState{Branch: "main"}, OK, "чисто"},
		{sysinfo.GitState{Branch: "main", Changes: 3}, OK, "3 изм"},
		{sysinfo.GitState{Branch: "main", Ahead: 2}, Warn, "2 не отпр"},
		{sysinfo.GitState{Branch: "main", Changes: 3, Ahead: 2}, Warn, "3 изм, 2 не отпр"},
	}
	for _, c := range cases {
		got := GitCheck(c.g)
		if got.Level != c.want || got.Text != c.text {
			t.Errorf("%+v -> %v %q, ожидали %v %q", c.g, got.Level, got.Text, c.want, c.text)
		}
	}
}

func TestMailCheck(t *testing.T) {
	if c := MailCheck("bad", []string{"нет DMARC"}); c.Level != Bad || c.Detail == "" {
		t.Errorf("bad: %+v", c)
	}
	if c := MailCheck("ok", nil); c.Level != OK || c.Text != "ок" || c.Detail != "" {
		t.Errorf("ok: %+v", c)
	}
	// Причины без уровня в подробности не идут: иначе у здорового домена появится разбор.
	if c := MailCheck("ok", []string{"что-то"}); c.Detail != "" {
		t.Errorf("лишнее пояснение: %q", c.Detail)
	}
}
