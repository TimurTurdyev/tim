// Package cronx: разбор пятипольных cron-выражений, ближайшие запуски и описание по-русски.
package cronx

import (
	"errors"
	"strconv"
	"strings"
	"time"
)

var macros = map[string]string{
	"@yearly": "0 0 1 1 *", "@annually": "0 0 1 1 *", "@monthly": "0 0 1 * *",
	"@weekly": "0 0 * * 0", "@daily": "0 0 * * *", "@midnight": "0 0 * * *", "@hourly": "0 * * * *",
}

var monNames = map[string]int{"jan": 1, "feb": 2, "mar": 3, "apr": 4, "may": 5, "jun": 6, "jul": 7, "aug": 8, "sep": 9, "oct": 10, "nov": 11, "dec": 12}

var dowNames = map[string]int{"sun": 0, "mon": 1, "tue": 2, "wed": 3, "thu": 4, "fri": 5, "sat": 6}

var monRu = []string{"", "январь", "февраль", "март", "апрель", "май", "июнь", "июль", "август", "сентябрь", "октябрь", "ноябрь", "декабрь"}

var dowRu = []string{"воскресенье", "понедельник", "вторник", "среда", "четверг", "пятница", "суббота"}

type field struct {
	vals map[int]bool
	any  bool
	raw  string
}

// Schedule: разобранное выражение.
type Schedule struct {
	min, hour, dom, mon, dow field
}

func Parse(expr string) (Schedule, error) {
	e := strings.TrimSpace(strings.ToLower(expr))
	if m, ok := macros[e]; ok {
		e = m
	}
	f := strings.Fields(e)
	if len(f) != 5 {
		return Schedule{}, errors.New("нужно 5 полей: минута час день месяц день_недели")
	}
	var s Schedule
	var err error
	specs := []struct {
		dst      *field
		min, max int
		names    map[string]int
		label    string
	}{
		{&s.min, 0, 59, nil, "минута"}, {&s.hour, 0, 23, nil, "час"}, {&s.dom, 1, 31, nil, "день"},
		{&s.mon, 1, 12, monNames, "месяц"}, {&s.dow, 0, 7, dowNames, "день недели"},
	}
	for i, field := range specs {
		if *field.dst, err = parseField(f[i], field.min, field.max, field.names); err != nil {
			return Schedule{}, errors.New(field.label + ": " + err.Error())
		}
	}
	if s.dow.vals[7] {
		s.dow.vals[0] = true
		delete(s.dow.vals, 7)
	}
	return s, nil
}

func parseField(s string, lo, hi int, names map[string]int) (field, error) {
	f := field{vals: map[int]bool{}, raw: s, any: s == "*"}
	for _, part := range strings.Split(s, ",") {
		rng, stepS, hasStep := strings.Cut(part, "/")
		step := 1
		if hasStep {
			n, err := strconv.Atoi(stepS)
			if err != nil || n < 1 {
				return f, errors.New("шаг " + stepS)
			}
			step = n
		}
		a, b := lo, hi
		if rng != "*" {
			x, y, isRange := strings.Cut(rng, "-")
			var err error
			if a, err = num(x, names); err != nil {
				return f, err
			}
			b = a
			if isRange {
				if b, err = num(y, names); err != nil {
					return f, err
				}
			} else if hasStep {
				b = hi
			}
		}
		if a < lo || b > hi || a > b {
			return f, errors.New("вне " + strconv.Itoa(lo) + "-" + strconv.Itoa(hi) + ": " + part)
		}
		for v := a; v <= b; v += step {
			f.vals[v] = true
		}
	}
	return f, nil
}

func num(s string, names map[string]int) (int, error) {
	if v, ok := names[s]; ok {
		return v, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, errors.New("не число: " + s)
	}
	return n, nil
}

func (s Schedule) dayMatches(t time.Time) bool {
	dom, dow := s.dom.vals[t.Day()], s.dow.vals[int(t.Weekday())]
	switch {
	case s.dom.any && s.dow.any:
		return true
	case s.dom.any:
		return dow
	case s.dow.any:
		return dom
	}
	return dom || dow
}

// Next: ближайшие n запусков после from (не раньше следующей минуты), поиск не дальше 5 лет.
func (s Schedule) Next(from time.Time, n int) []time.Time {
	var out []time.Time
	t := from.Truncate(time.Minute).Add(time.Minute)
	limit := from.AddDate(5, 0, 0)
	for len(out) < n && t.Before(limit) {
		switch {
		case !s.mon.vals[int(t.Month())]:
			t = time.Date(t.Year(), t.Month()+1, 1, 0, 0, 0, 0, t.Location())
		case !s.dayMatches(t):
			t = time.Date(t.Year(), t.Month(), t.Day()+1, 0, 0, 0, 0, t.Location())
		case !s.hour.vals[t.Hour()]:
			t = time.Date(t.Year(), t.Month(), t.Day(), t.Hour()+1, 0, 0, 0, t.Location())
		case !s.min.vals[t.Minute()]:
			t = t.Add(time.Minute)
		default:
			out = append(out, t)
			t = t.Add(time.Minute)
		}
	}
	return out
}

// Describe: человеческое описание выражения.
func (s Schedule) Describe() string {
	var when string
	switch {
	case s.min.any && s.hour.any:
		when = "каждую минуту"
	case strings.HasPrefix(s.min.raw, "*/") && s.hour.any:
		when = "каждые " + strings.TrimPrefix(s.min.raw, "*/") + " мин"
	case len(s.min.vals) == 1 && len(s.hour.vals) == 1:
		when = "в " + clock(first(s.hour), first(s.min))
	case len(s.min.vals) == 1 && s.hour.any:
		when = "в " + strconv.Itoa(first(s.min)) + "-ю минуту каждого часа"
	case len(s.min.vals) == 1:
		when = "в " + pad(first(s.min)) + " минут, часы " + list(s.hour, nil, "")
	default:
		when = "минуты " + list(s.min, nil, "каждую") + ", часы " + list(s.hour, nil, "каждый")
	}
	var days []string
	if !s.dom.any {
		days = append(days, "числа "+list(s.dom, nil, ""))
	}
	if !s.dow.any {
		days = append(days, "по дням: "+list(s.dow, dowRu, ""))
	}
	day := "каждый день"
	switch len(days) {
	case 1:
		day = days[0]
	case 2:
		day = days[0] + " или " + days[1]
	}
	out := when + ", " + day
	if !s.mon.any {
		out += ", месяц " + list(s.mon, monRu, "")
	}
	return out
}

func clock(h, m int) string { return pad(h) + ":" + pad(m) }

func pad(n int) string {
	if n < 10 {
		return "0" + strconv.Itoa(n)
	}
	return strconv.Itoa(n)
}

func first(f field) int {
	for v := 0; v <= 59; v++ {
		if f.vals[v] {
			return v
		}
	}
	return 0
}

func list(f field, names []string, every string) string {
	if f.any && every != "" {
		return every
	}
	if strings.HasPrefix(f.raw, "*/") {
		return "каждые " + strings.TrimPrefix(f.raw, "*/")
	}
	var out []string
	for v := 0; v <= 59; v++ {
		if !f.vals[v] {
			continue
		}
		if names != nil && v < len(names) {
			out = append(out, names[v])
		} else {
			out = append(out, strconv.Itoa(v))
		}
	}
	return strings.Join(out, ", ")
}
