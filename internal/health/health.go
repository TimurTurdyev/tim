// Package health: сведение частных проверок проекта в одну строку обхода.
package health

import (
	"fmt"
	"sort"
	"time"
)

// Level: насколько всё плохо. Порядок важен: худший уровень - наибольший.
type Level int

const (
	Skip Level = iota // проверить нечем, например у проекта нет url
	OK
	Warn
	Bad
)

func (l Level) String() string {
	switch l {
	case Bad:
		return "плохо"
	case Warn:
		return "внимание"
	case OK:
		return "ок"
	}
	return "-"
}

// MarshalText: в --json уровень словом, а не числом.
func (l Level) MarshalText() ([]byte, error) {
	return []byte([...]string{"skip", "ok", "warn", "bad"}[l]), nil
}

// Check: одна проверка. Text идёт в колонку таблицы, Detail - в разбор проблем под ней.
type Check struct {
	Name   string
	Level  Level
	Text   string
	Detail string
}

// Row: все проверки одного проекта.
type Row struct {
	Name   string
	Checks []Check
}

func (r Row) Level() Level {
	worst := Skip
	for _, c := range r.Checks {
		if c.Level > worst {
			worst = c.Level
		}
	}
	return worst
}

func (r Row) Get(name string) Check {
	for _, c := range r.Checks {
		if c.Name == name {
			return c
		}
	}
	return Check{Name: name, Level: Skip, Text: "-"}
}

// Problems: проверки, о которых есть что сказать подробнее.
func (r Row) Problems() []Check {
	var out []Check
	for _, c := range r.Checks {
		if c.Level >= Warn && c.Detail != "" {
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
	days := int(time.Until(t).Hours() / 24)
	c := Check{Name: name, Level: OK, Text: fmt.Sprintf("%d дн", days)}
	switch {
	case days < 0:
		c.Level, c.Text = Bad, fmt.Sprintf("истёк %d дн назад", -days)
		c.Detail = fmt.Sprintf("%s истёк %s", name, t.Format("2006-01-02"))
	case days <= bad:
		c.Level = Bad
		c.Detail = fmt.Sprintf("%s истекает %s, осталось %d дн", name, t.Format("2006-01-02"), days)
	case days <= warn:
		c.Level = Warn
		c.Detail = fmt.Sprintf("%s истекает %s, осталось %d дн", name, t.Format("2006-01-02"), days)
	}
	return c
}

// Counts: сколько строк каждого уровня.
func Counts(rows []Row) (ok, warn, bad int) {
	for _, r := range rows {
		switch r.Level() {
		case Bad:
			bad++
		case Warn:
			warn++
		case OK:
			ok++
		}
	}
	return
}
