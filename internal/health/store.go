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

// Кэш проверок: последняя проверка каждого проекта с её временем. Пишут check, экраны Сайтов и
// Проверки; читают ВНИМАНИЕ на главном, бейджи разделов и Сайты, чтобы открываться сразу, а не
// ждать сети.

// StaleAfter: проверка старше суток показывается янтарной давностью - её пора повторить.
const StaleAfter = 24 * time.Hour

// Saved: проверка из кэша и когда она была.
type Saved struct {
	Row
	At time.Time
}

func checksDir() string { return paths.State("checks") }

// SaveRows запоминает проверки; в dry-run ничего не пишет. Проверка без сети (--local) не стирает
// сетевые: несделанная проверка оставляет прежний ответ.
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
	for _, c := range old.Checks {
		if n := fresh.Get(c.Name); n.Level != level.Skip {
			c = n
		}
		out.Checks = append(out.Checks, c)
	}
	for _, c := range fresh.Checks {
		if !hasCheck(old, c.Name) {
			out.Checks = append(out.Checks, c)
		}
	}
	return out
}

func hasCheck(r Row, name string) bool {
	for _, c := range r.Checks {
		if c.Name == name {
			return true
		}
	}
	return false
}

// SavedRow: последняя проверка проекта.
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
