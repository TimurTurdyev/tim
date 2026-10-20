// Package health: сведение частных проверок проекта в одну строку обхода.
package health

import (
	"fmt"
	"sort"
	"time"

	"github.com/TimurTurdyev/tim/internal/level"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// Check: одна проверка. Text идёт в колонку таблицы, Detail - в разбор проблем под ней, Fix - с чего
// начать: команда или место, где смотреть. Решает правило, экран и команда только печатают.
type Check struct {
	Name   string
	Level  level.Level
	Text   string
	Detail string
	Fix    string `json:",omitempty"`
}

// Row: все проверки одного проекта.
type Row struct {
	Name   string
	Checks []Check
}

func (r Row) Level() level.Level {
	worst := level.Skip
	for _, c := range r.Checks {
		worst = max(worst, c.Level)
	}
	return worst
}

func (r Row) Get(name string) Check {
	for _, c := range r.Checks {
		if c.Name == name {
			return c
		}
	}
	return Check{Name: name, Level: level.Skip, Text: "-"}
}

// Problems: проверки, о которых есть что сказать подробнее.
func (r Row) Problems() []Check {
	var out []Check
	for _, c := range r.Checks {
		if c.Level >= level.Warn && c.Detail != "" {
			out = append(out, c)
		}
	}
	return out
}

// Sort: проблемные строки наверх, внутри уровня по имени.
func Sort(rows []Row) {
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i].Level(), rows[j].Level()
		if a != b {
			return a > b
		}
		return rows[i].Name < rows[j].Name
	})
}

// Deadline: проверка на срок - сертификат, регистрация домена. warn и bad в днях.
func Deadline(name string, t time.Time, warn, bad int) Check {
	return DeadlineAt(name, t, time.Now(), warn, bad)
}

// DeadlineAt как Deadline, но на момент now. Остаток считается вверх: 12 часов - это "1 дн",
// истёкшим срок становится, только когда прошёл.
func DeadlineAt(name string, t, now time.Time, warn, bad int) Check {
	days := ui.DaysUntil(t, now)
	c := Check{Name: name, Level: level.OK, Text: fmt.Sprintf("%d дн", days)}
	switch {
	case days <= 0:
		c.Level, c.Text = level.Bad, ui.Expired(days)
		c.Detail = fmt.Sprintf("%s истёк %s", name, t.Format("2006-01-02"))
	case days <= bad:
		c.Level = level.Bad
		c.Detail = fmt.Sprintf("%s истекает %s, осталось %d дн", name, t.Format("2006-01-02"), days)
	case days <= warn:
		c.Level = level.Warn
		c.Detail = fmt.Sprintf("%s истекает %s, осталось %d дн", name, t.Format("2006-01-02"), days)
	}
	return c
}

// Counts: сколько строк каждого уровня.
func Counts(rows []Row) (ok, warn, bad int) {
	for _, r := range rows {
		switch r.Level() {
		case level.Bad:
			bad++
		case level.Warn:
			warn++
		case level.OK:
			ok++
		}
	}
	return
}
