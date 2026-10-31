package health

import (
	"os"
	"path/filepath"
	"time"

	"github.com/TimurTurdyev/tim/internal/cache"
	"github.com/TimurTurdyev/tim/internal/level"
	"github.com/TimurTurdyev/tim/internal/paths"
	"github.com/TimurTurdyev/tim/internal/runner"
)

// Кэш проверок: последняя проверка каждого сайта и сервера с её временем. Пишут check и экран Сайтов;
// читают ВНИМАНИЕ на главном, бейджи разделов и Сайты, чтобы открываться сразу, а не ждать сети.

// StaleAfter: проверка старше суток показывается янтарной давностью - её пора повторить.
const StaleAfter = 24 * time.Hour

// Saved: проверка из кэша и когда она была.
type Saved struct {
	Row
	At time.Time
}

func checksDir() string { return paths.State("checks") }

// SaveRows запоминает проверки; в dry-run ничего не пишет. Несделанная проверка оставляет прежний
// ответ, а проверки, которых в строке больше нет (git, секреты), из кэша уходят.
func SaveRows(rows []Row) {
	if runner.DryRun {
		return
	}
	for _, r := range rows {
		if old, ok := SavedRow(r.Name); ok {
			r = merge(old.Row, r)
		}
		_ = cache.Store(cache.Path(checksDir(), r.Name), r, 0o644)
	}
}

func merge(old, fresh Row) Row {
	out := Row{Name: fresh.Name}
	for _, c := range fresh.Checks {
		if prev := old.Get(c.Name); c.Level == level.Skip && prev.Level != level.Skip {
			c = prev
		}
		out.Checks = append(out.Checks, c)
	}
	return out
}

// SavedRow: последняя проверка сайта или сервера.
func SavedRow(name string) (Saved, bool) {
	e, ok := cache.Load[Row](cache.Path(checksDir(), name))
	if !ok {
		return Saved{}, false
	}
	return Saved{Row: e.Value, At: e.Fetched}, true
}

// SavedRows: все запомненные проверки.
func SavedRows() []Saved {
	files, _ := os.ReadDir(checksDir())
	var out []Saved
	for _, f := range files {
		if filepath.Ext(f.Name()) != ".json" {
			continue
		}
		if e, ok := cache.Load[Row](filepath.Join(checksDir(), f.Name())); ok {
			out = append(out, Saved{Row: e.Value, At: e.Fetched})
		}
	}
	return out
}

// Alert: беда для группы ВНИМАНИЕ главного экрана - уровень и текст "<объект>: <что>".
type Alert struct {
	Level level.Level
	Text  string
}
