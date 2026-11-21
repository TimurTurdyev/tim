package sites

import (
	"strconv"
	"strings"

	"github.com/TimurTurdyev/tim/internal/domaudit"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// sourceLines: что нашли сами, что добавили сторонние сервисы и кто из них не ответил; dates - с датой кэша.
func sourceLines(rep *domaudit.Report, dates bool) []string {
	var own, others, silent []string
	asked := false
	for _, s := range rep.Sources {
		if s.Own {
			own = append(own, ownSourceText(s))
			continue
		}
		asked = true
		text := s.Name + " " + strconv.Itoa(s.Names)
		if s.Cut {
			text += " (отдал не всё)"
		}
		switch {
		case s.Error != "" && s.Cached.IsZero():
			silent = append(silent, s.Name+": "+s.Error)
		case s.Error != "":
			silent = append(silent, s.Name+": "+s.Error+", показан список от "+s.Cached.Local().Format("02.01"))
			others = append(others, text)
		case dates && !s.Cached.IsZero():
			others = append(others, text+" (кэш "+s.Cached.Local().Format("02.01 15:04")+")")
		default:
			others = append(others, text)
		}
	}
	var out []string
	for _, l := range []struct {
		title string
		list  []string
		sep   string
	}{{"сам: ", own, ", "}, {"сторонние: ", others, ", "}, {"не ответили: ", silent, "; "}} {
		if len(l.list) > 0 {
			out = append(out, l.title+strings.Join(l.list, l.sep))
		}
	}
	if total := foundText(rep.Found, asked); total != "" {
		out = append(out, total)
	}
	return out
}

// ownSourceText: свой источник - сколько имён назвал и как работал.
func ownSourceText(s domaudit.SourceState) string {
	var how []string
	if s.Total > 0 {
		how = append(how, "проверено "+thousands(s.Checked)+" из "+thousands(s.Total))
	}
	for _, more := range []string{s.Note, s.Error} {
		if more != "" {
			how = append(how, more)
		}
	}
	text := s.Name + " " + strconv.Itoa(s.Names)
	if len(how) > 0 {
		text += " (" + strings.Join(how, ", ") + ")"
	}
	return text
}

// foundText: итог - сколько имён отчёта нашли сами и сколько добавили сторонние; asked - их спрашивали.
func foundText(f domaudit.Found, asked bool) string {
	names := func(n int) string { return ui.Count(n, "имя", "имени", "имён") }
	switch total := f.Own + f.Added; {
	case total == 0:
		return ""
	case !asked:
		return "итог: своими силами " + names(f.Own) + ", сторонние сервисы не спрашивались"
	case f.Added == 0:
		return "итог: своими силами " + names(f.Own) + " из " + strconv.Itoa(total) + ", сторонние ничего не добавили"
	default:
		return "итог: своими силами " + names(f.Own) + " из " + strconv.Itoa(total) + ", сторонние добавили " + strconv.Itoa(f.Added)
	}
}

// thousands: число с пробелами между тысячами.
func thousands(n int) string {
	s := strconv.Itoa(n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + " " + s[i:]
	}
	return s
}
