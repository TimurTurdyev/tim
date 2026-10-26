// Package logsum: сведение строк журнала в шаблоны - числа, адреса и id заменены, у шаблона счётчик,
// первое и последнее время. Сотни одинаковых ошибок становятся тремя-четырьмя строками.
package logsum

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
)

var stamps = []stamp{
	// journalctl -o short-iso, Laravel [2026-10-28 03:12:01], ISO с долями и поясом.
	{re: regexp.MustCompile(`^\[?(\d{4}-\d{2}-\d{2}[T ]\d{2}:\d{2}:\d{2})(?:[.,]\d+)?(?:Z|[+-]\d{2}:?\d{2})?\]?\s*`),
		layout: "2006-01-02T15:04:05", cleanup: func(s string) string { return strings.Replace(s, " ", "T", 1) }},
	// nginx error.log
	{re: regexp.MustCompile(`^(\d{4}/\d{2}/\d{2} \d{2}:\d{2}:\d{2})\s*`), layout: "2006/01/02 15:04:05"},
	// PHP: [28-Oct-2026 03:12:01 UTC]
	{re: regexp.MustCompile(`^\[(\d{2}-[A-Z][a-z]{2}-\d{4} \d{2}:\d{2}:\d{2})[^\]]*\]\s*`), layout: "02-Jan-2006 15:04:05"},
	// syslog: Oct 28 03:12:01 host
	{re: regexp.MustCompile(`^([A-Z][a-z]{2} [ \d]\d \d{2}:\d{2}:\d{2})\s+`), layout: "Jan _2 15:04:05", host: true, noYear: true},
}

var (
	journalHost = regexp.MustCompile(`^\S+ (\S+?(\[\d+\])?: )`)
	continued   = regexp.MustCompile(`^(\s|#\d+ |Stack trace:|\[stacktrace\]|\}|"\}|at \S+\()`)
	reUUID      = regexp.MustCompile(`(?i)\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\b`)
	reIPv4      = regexp.MustCompile(`\b\d{1,3}(\.\d{1,3}){3}(:\d+)?\b`)
	reIPv6      = regexp.MustCompile(`(?i)\b[0-9a-f]{0,4}(:[0-9a-f]{0,4}){2,7}\b`)
	reHex       = regexp.MustCompile(`(?i)\b(0x[0-9a-f]+|[0-9a-f]*[a-f][0-9a-f]*)\b`)
	reNum       = regexp.MustCompile(`(^|[^\w.])\d+(\.\d+)*\b`)
)

// Pattern: шаблон строки журнала.
type Pattern struct {
	Text        string    `json:"text"`
	Sample      string    `json:"sample"` // первая строка без времени, но с настоящими числами
	Count       int       `json:"count"`
	First, Last time.Time `json:"-"` // нулевые, если у строк нет времени
	order       int
}

// stamp: время в начале строки и как его разобрать. Порядок важен: ISO раньше короткого syslog.
type stamp struct {
	re      *regexp.Regexp
	layout  string
	host    bool // за временем имя хоста, его в шаблон не берём (journalctl, syslog)
	noYear  bool
	cleanup func(string) string
}

// strip: строка без времени и хоста; at - время, если удалось разобрать.
func strip(line string) (text string, at time.Time) {
	line = strings.TrimSpace(line)
	for _, s := range stamps {
		m := s.re.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		raw := m[1]
		if s.cleanup != nil {
			raw = s.cleanup(raw)
		}
		if t, err := time.ParseInLocation(s.layout, raw, time.Local); err == nil {
			if s.noYear {
				t = t.AddDate(time.Now().Year(), 0, 0)
			}
			at = t
		}
		line = line[len(m[0]):]
		if s.host || strings.Contains(m[0], "T") {
			if h := journalHost.FindStringSubmatch(line); h != nil {
				line = line[len(h[0])-len(h[1]):]
			}
		}
		break
	}
	return line, at
}

// Template: строка без времени и хоста, изменчивые части заменены: id, адреса, хэши, числа.
func Template(line string) (text string, at time.Time) {
	text, at = strip(line)
	text = reUUID.ReplaceAllString(text, "<id>")
	text = reIPv4.ReplaceAllString(text, "<ip>")
	text = reIPv6.ReplaceAllStringFunc(text, func(m string) string {
		// Время 03:12:01 тоже из двоеточий; адрес - с буквой или ::.
		if strings.Contains(m, "::") || strings.ContainsAny(strings.ToLower(m), "abcdef") {
			return "<ip>"
		}
		return m
	})
	text = reHex.ReplaceAllStringFunc(text, func(m string) string {
		// Хэш - из цифр и букв a-f, не короче 6; слова вроде "face" и "added" остаются.
		if strings.HasPrefix(strings.ToLower(m), "0x") || len(m) >= 6 && strings.ContainsAny(m, "0123456789") {
			return "<hex>"
		}
		return m
	})
	// Число внутри имени (php8.3-fpm) остаётся: перед ним буква или точка.
	return reNum.ReplaceAllString(text, "${1}N"), at
}

// Summarize: шаблоны по убыванию счётчика; строки-продолжения (стек вызовов) не считаются.
func Summarize(lines []string) []Pattern {
	byText := map[string]*Pattern{}
	var out []*Pattern
	for _, l := range lines {
		if strings.TrimSpace(l) == "" || continued.MatchString(l) || strings.HasPrefix(l, "-- ") {
			continue
		}
		text, at := Template(l)
		p := byText[text]
		if p == nil {
			plain, _ := strip(l)
			p = &Pattern{Text: text, Sample: plain, order: len(out)}
			byText[text] = p
			out = append(out, p)
		}
		p.Count++
		if !at.IsZero() {
			if p.First.IsZero() || at.Before(p.First) {
				p.First = at
			}
			if at.After(p.Last) {
				p.Last = at
			}
		}
	}
	slices.SortStableFunc(out, func(a, b *Pattern) int {
		if a.Count != b.Count {
			return b.Count - a.Count
		}
		return a.order - b.order
	})
	res := make([]Pattern, len(out))
	for i, p := range out {
		res[i] = *p
	}
	return res
}

// Format: шаблоны строками - счётчик, когда, текст; total - сколько строк было на входе.
func Format(pats []Pattern, total int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d %s -> %d %s\n\n", total, plural(total, "строка", "строки", "строк"), len(pats),
		plural(len(pats), "шаблон", "шаблона", "шаблонов"))
	for _, p := range pats {
		text := p.Text
		if p.Count == 1 {
			text = p.Sample
		}
		if r := []rune(text); len(r) > 220 {
			text = string(r[:219]) + "..."
		}
		fmt.Fprintf(&b, "%6d  %-23s  %s\n", p.Count, when(p), text)
	}
	return b.String()
}

// when: время первой и последней строки; в один день - дата один раз.
func when(p Pattern) string {
	switch {
	case p.First.IsZero():
		return ""
	case p.Count == 1 || p.First.Equal(p.Last):
		return p.First.Format("02.01 15:04")
	case p.First.YearDay() == p.Last.YearDay():
		return p.First.Format("02.01 15:04") + "-" + p.Last.Format("15:04")
	}
	return p.First.Format("02.01 15:04") + "-" + p.Last.Format("02.01 15:04")
}

func plural(n int, one, few, many string) string {
	switch {
	case n%10 == 1 && n%100 != 11:
		return one
	case n%10 >= 2 && n%10 <= 4 && (n%100 < 10 || n%100 >= 20):
		return few
	}
	return many
}
