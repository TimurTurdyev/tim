package health

import (
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

func TestGitCheck(t *testing.T) {
	ignored := EnvGit{Exists: true, Ignored: true}
	cases := []struct {
		g    sysinfo.GitState
		env  EnvGit
		want Level
		text string
	}{
		{sysinfo.GitState{}, EnvGit{}, OK, "чисто"},
		{sysinfo.GitState{Changes: 3}, ignored, OK, "3 изм"},
		{sysinfo.GitState{Ahead: 2}, ignored, Warn, "2 не отпр"},
		{sysinfo.GitState{Behind: 4}, ignored, Warn, "4 позади"},
		{sysinfo.GitState{Changes: 3, Ahead: 2}, ignored, Warn, "3 изм, 2 не отпр"},
		{sysinfo.GitState{}, EnvGit{Exists: true}, Warn, ".env не в ignore"},
		// В репозитории - хуже всего, даже если игнор потом добавили: файл уже в истории.
		{sysinfo.GitState{Changes: 1}, EnvGit{Exists: true, Tracked: true, Ignored: true}, Bad, ".env в git, 1 изм"},
	}
	for _, c := range cases {
		got := GitCheck(c.g, c.env)
		if got.Level != c.want || got.Text != c.text {
			t.Errorf("%+v %+v -> %v %q, ожидали %v %q", c.g, c.env, got.Level, got.Text, c.want, c.text)
		}
		if got.Level == Bad && got.Detail == "" {
			t.Errorf("%+v: нет пояснения", c.env)
		}
	}
}
