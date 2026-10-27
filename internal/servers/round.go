package servers

import (
	"fmt"
	"slices"
	"strings"

	"github.com/TimurTurdyev/tim/internal/baseline"
	"github.com/TimurTurdyev/tim/internal/cache"
	"github.com/TimurTurdyev/tim/internal/health"
	"github.com/TimurTurdyev/tim/internal/level"
	"github.com/TimurTurdyev/tim/internal/paths"
	"github.com/TimurTurdyev/tim/internal/runner"
)

// Обход по расписанию идёт через тот же сбор, что экран сервера: кэш, новое с прошлого раза, проверки
// снаружи. Сервер, который не ответил, тревожит только со второго обхода подряд: одна сетевая заминка
// не должна будить ночью.

func missPath(target string) string { return cache.Path(paths.State("servers-miss"), hostKey(target)) }

// misses: сколько обходов подряд сервер не отвечал; ok - ответил, счёт сбрасывается.
func misses(target string, ok bool) int {
	n := 0
	if !ok {
		e, _ := cache.Load[int](missPath(target))
		n = e.Value + 1
	}
	if !runner.DryRun {
		_ = cache.Store(missPath(target), n, 0o644)
	}
	return n
}

// CheckServer: сервер одной ячейкой обхода - худшая находка, новое с прошлого раза, недоступность.
func CheckServer(alias string) health.Check {
	s := collect(Server{Alias: alias})
	c := health.Check{Name: "сервер", Level: level.OK, Text: "ок"}
	if s.Err != "" {
		c.Level, c.Text, c.Detail = level.Warn, "не ответил", alias+": "+s.Err+"; маршрут и вход проверит tim ssh "+alias
		if misses(alias, false) >= 2 {
			c.Level = level.Bad
		}
		return c
	}
	misses(alias, true)
	fresh := baseline.Pending(object(alias))
	var worst *health.Check
	for i := range s.Findings {
		if f := &s.Findings[i]; f.Level >= level.Warn && (worst == nil || f.Level > worst.Level) {
			worst = f
		}
	}
	switch {
	case worst != nil && worst.Level == level.Bad:
		c.Level, c.Text, c.Detail, c.Fix = level.Bad, worst.Name+" "+worst.Text, alias+": "+worst.Detail, worst.Fix
	case len(fresh) > 0:
		c.Level, c.Text, c.Detail = level.Warn, fmt.Sprintf("новое: %d", len(fresh)), alias+": "+newWhat(fresh[0])+" "+newText(fresh[0])
		c.Fix = "своё - tim server " + alias + " --accept; что делать с чужим - на экране tim server " + alias
	case worst != nil:
		c.Level, c.Text, c.Detail, c.Fix = worst.Level, worst.Name+" "+worst.Text, alias+": "+worst.Detail, worst.Fix
	}
	return c
}

// RoundLines: строки уведомления по серверу - плохое и новое; недоступный - со второго обхода подряд.
func RoundLines(alias string) []string {
	s, _, ok := saved(alias)
	e, _ := cache.Load[int](missPath(alias))
	if e.Value >= 2 {
		return []string{fmt.Sprintf("✕ %s: не отвечает %d обхода подряд", alias, e.Value)}
	}
	if !ok {
		return nil
	}
	var out []string
	for _, f := range s.Findings {
		if f.Level == level.Bad {
			out = append(out, "✕ "+alias+": "+f.Name+" "+f.Text)
		}
	}
	if news := pendingWords(alias); len(news) > 0 {
		out = append(out, "▲ "+alias+": "+strings.Join(news, ", "))
	}
	return out
}

// pendingWords: что нового на сервере словами - для главного экрана и уведомления.
func pendingWords(alias string) []string {
	var news []string
	for _, it := range baseline.Pending(object(alias)) {
		if w := newPhrase(it); !slices.Contains(news, w) {
			news = append(news, w)
		}
	}
	return news
}

// newPhrase: "новый ключ ssh root", "сменился ключ хоста".
func newPhrase(it baseline.Item) string {
	switch what := newWhat(it); {
	case strings.HasPrefix(it.Kind, "cron"):
		return "новая строка крона"
	case it.Kind == "hostkey":
		return "сменился ключ хоста"
	case it.Kind == "login-net":
		return what
	default:
		return "новый " + what
	}
}
