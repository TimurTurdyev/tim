// Package cronx: разбор пятипольных cron-выражений, ближайшие запуски и описание по-русски.
package cronx

import (
	"errors"
	"slices"
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

// dowDat: "по понедельникам".
var dowDat = []string{"воскресеньям", "понедельникам", "вторникам", "средам", "четвергам", "пятницам", "субботам"}

// monPrep: "в январе".
var monPrep = []string{"", "январе", "феврале", "марте", "апреле", "мае", "июне", "июле", "августе", "сентябре", "октябре", "ноябре", "декабре"}

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

// Describe: человеческое описание выражения - "каждые 15 минут с 09:00 до 18:45, по будням".
func (s Schedule) Describe() string {
	out := s.when() + ", " + s.days()
	if !s.mon.any {
		out += ", " + months(s.mon)
	}
	return out
}

// when: в какие минуты и часы.
func (s Schedule) when() string {
	mins, hours := values(s.min, 59), values(s.hour, 23)
	step := strings.HasPrefix(s.min.raw, "*/")
	switch {
	case s.min.any && s.hour.any:
		return "каждую минуту"
	case step && s.hour.any:
		return "каждые " + strings.TrimPrefix(s.min.raw, "*/") + " минут"
	case step && contiguous(hours):
		return "каждые " + strings.TrimPrefix(s.min.raw, "*/") + " минут с " + clock(hours[0], mins[0]) + " до " + clock(hours[len(hours)-1], mins[len(mins)-1])
	case len(mins) != 1:
		return "минуты " + list(s.min, nil, "каждую") + ", часы " + list(s.hour, nil, "каждый")
	case s.hour.any:
		return "каждый час в :" + pad(mins[0])
	case strings.HasPrefix(s.hour.raw, "*/"):
		return "каждые " + strings.TrimPrefix(s.hour.raw, "*/") + " ч в :" + pad(mins[0])
	case len(hours) <= 4:
		var at []string
		for _, h := range hours {
			at = append(at, clock(h, mins[0]))
		}
		return "в " + strings.Join(at, ", ")
	case contiguous(hours):
		return "каждый час в :" + pad(mins[0]) + " с " + clock(hours[0], mins[0]) + " до " + clock(hours[len(hours)-1], mins[0])
	}
	return "в " + pad(mins[0]) + " минут, часы " + list(s.hour, nil, "")
}

// days: в какие дни. Если заданы и числа, и дни недели, cron запускает по любому из них.
func (s Schedule) days() string {
	var parts []string
	if !s.dom.any {
		d := values(s.dom, 31)
		if len(d) == 1 {
			parts = append(parts, strconv.Itoa(d[0])+"-го числа")
		} else {
			parts = append(parts, "числа "+list(s.dom, nil, ""))
		}
	}
	if !s.dow.any {
		parts = append(parts, weekdays(values(s.dow, 6), s.dow))
	}
	if len(parts) == 0 {
		return "каждый день"
	}
	return strings.Join(parts, " или ")
}

func weekdays(d []int, f field) string {
	switch {
	case slices.Equal(d, []int{1, 2, 3, 4, 5}):
		return "по будням"
	case slices.Equal(d, []int{0, 6}):
		return "по выходным"
	case len(d) <= 3:
		var names []string
		for _, v := range d {
			names = append(names, dowDat[v])
		}
		return "по " + strings.Join(names, " и ")
	}
	return "по дням: " + list(f, dowRu, "")
}

func months(f field) string {
	m := values(f, 12)
	if len(m) <= 3 {
		var names []string
		for _, v := range m {
			names = append(names, monPrep[v])
		}
		return "в " + strings.Join(names, " и ")
	}
	return "месяцы " + list(f, monRu, "")
}

// Fields: поля выражения по порядку - имя, как записано, что значит.
func (s Schedule) Fields() [][3]string {
	return [][3]string{
		{"минута", s.min.raw, meaning(s.min, nil, "любая", "минут", 59)},
		{"час", s.hour.raw, meaning(s.hour, nil, "любой", "ч", 23)},
		{"день месяца", s.dom.raw, meaning(s.dom, nil, "любой", "дн", 31)},
		{"месяц", s.mon.raw, meaning(s.mon, monRu, "любой", "мес", 12)},
		{"день недели", s.dow.raw, meaning(s.dow, dowRu, "любой", "дн", 6)},
	}
}

func meaning(f field, names []string, anyWord, unit string, max int) string {
	switch v := values(f, max); {
	case f.any:
		return anyWord
	case strings.HasPrefix(f.raw, "*/"):
		return "каждые " + strings.TrimPrefix(f.raw, "*/") + " " + unit
	case names == nil && contiguous(v) && len(v) > 2:
		return "с " + strconv.Itoa(v[0]) + " до " + strconv.Itoa(v[len(v)-1])
	}
	return list(f, names, "")
}

// values: значения поля по возрастанию.
func values(f field, max int) []int {
	var out []int
	for v := 0; v <= max; v++ {
		if f.vals[v] {
			out = append(out, v)
		}
	}
	return out
}

func contiguous(v []int) bool {
	for i := 1; i < len(v); i++ {
		if v[i] != v[i-1]+1 {
			return false
		}
	}
	return len(v) > 1
}

func clock(h, m int) string { return pad(h) + ":" + pad(m) }

func pad(n int) string {
	if n < 10 {
		return "0" + strconv.Itoa(n)
	}
	return strconv.Itoa(n)
}

func list(f field, names []string, every string) string {
	if f.any && every != "" {
		return every
	}
	if strings.HasPrefix(f.raw, "*/") {
		return "каждые " + strings.TrimPrefix(f.raw, "*/")
	}
	name := func(v int) string {
		if names != nil && v < len(names) {
			return names[v]
		}
		return strconv.Itoa(v)
	}
	// Подряд идущие - диапазоном: "9-18", а не десять чисел через запятую.
	var out []string
	for v := 0; v <= 59; v++ {
		if !f.vals[v] {
			continue
		}
		end := v
		for end < 59 && f.vals[end+1] {
			end++
		}
		switch {
		case end-v >= 2:
			out = append(out, name(v)+"-"+name(end))
		case end > v:
			out = append(out, name(v), name(end))
		default:
			out = append(out, name(v))
		}
		v = end
	}
	return strings.Join(out, ", ")
}
