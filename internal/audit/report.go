package audit

import (
	"fmt"
	"html"
	"strings"
	"time"

	"github.com/TimurTurdyev/tim/internal/level"
)

// Отчёт для клиента: по-русски, без жаргона в заголовках, у каждой находки почему и что сделать.

const reportCSS = `body{margin:0;background:#f6f7f9;color:#1f2328;font:16px/1.55 -apple-system,"Segoe UI",sans-serif}
main{max-width:860px;margin:0 auto;padding:40px 24px 60px}h1{font-size:28px;margin:0 0 8px}
.meta{color:#6b7280;margin:0 0 20px}.verdict{font-size:18px;font-weight:600;padding:14px 18px;border-radius:8px;background:#e8f5ee}
.verdict.bad{background:#fdecec}.verdict.warn{background:#fff4e0}h2{font-size:20px;margin:36px 0 12px;padding-left:12px;border-left:4px solid #2a9d8f}
h2.bad{border-color:#d64545}h2.warn{border-color:#e0a030}section{background:#fff;border:1px solid #e5e7eb;border-radius:8px;padding:14px 18px;margin:0 0 12px}
h3{font-size:17px;margin:0 0 8px}.area{display:inline-block;font-size:12px;text-transform:uppercase;letter-spacing:.04em;color:#6b7280;margin-right:8px}
p{margin:6px 0}code{font:14px/1.5 Menlo,monospace;background:#f3f4f6;padding:1px 5px;border-radius:4px;white-space:pre-wrap}
.foot{color:#9ca3af;font-size:13px;margin-top:40px}`

// groups: находки по важности, как их читает клиент.
var groups = []struct {
	level level.Level
	title string
}{
	{level.Bad, "Серьёзно - закрыть в первую очередь"},
	{level.Warn, "Стоит поправить"},
	{level.Info, "К сведению"},
}

// verdictWords: вердикт словами для отчёта.
func (r *Report) verdictWords() string {
	bad, warn := 0, 0
	for _, f := range r.Findings {
		switch f.Level {
		case level.Bad:
			bad++
		case level.Warn:
			warn++
		}
	}
	switch {
	case bad > 0:
		return fmt.Sprintf("Есть серьёзные проблемы: %d. Их стоит закрыть в первую очередь.", bad)
	case warn > 0:
		return fmt.Sprintf("Серьёзных проблем нет, есть что поправить: %d.", warn)
	}
	return "Серьёзных проблем нет."
}

// Markdown: отчёт для клиента - вставить в письмо или задачу.
func (r *Report) Markdown(now time.Time) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Проверка сайта %s\n\n", r.Final)
	fmt.Fprintf(&b, "Дата: %s. Проверено: %s.\n\n", now.Format("2006-01-02"), strings.Join(r.Checked, ", "))
	fmt.Fprintf(&b, "**%s**\n", r.verdictWords())
	for _, g := range groups {
		list := r.byLevel(g.level)
		if len(list) == 0 {
			continue
		}
		fmt.Fprintf(&b, "\n## %s\n", g.title)
		for _, f := range list {
			fmt.Fprintf(&b, "\n### %s: %s\n", f.Area, f.Text)
			if f.Why != "" {
				fmt.Fprintf(&b, "\nПочему это важно: %s.\n", strings.TrimRight(f.Why, "."))
			}
			if f.Fix != "" {
				fmt.Fprintf(&b, "\nЧто сделать: %s.\n", strings.TrimRight(f.Fix, "."))
			}
		}
	}
	if len(r.Findings) == 0 {
		b.WriteString("\nЗамечаний нет.\n")
	}
	return b.String()
}

// HTML: та же структура самодостаточной страницей - открыть у себя или отправить файлом.
func (r *Report) HTML(now time.Time) string {
	var b strings.Builder
	b.WriteString("<!doctype html><html lang=\"ru\"><head><meta charset=\"utf-8\"><title>Проверка сайта " + html.EscapeString(r.Final) + "</title>\n")
	b.WriteString("<style>" + reportCSS + "</style></head><body><main>\n")
	fmt.Fprintf(&b, "<h1>Проверка сайта %s</h1>\n", html.EscapeString(r.Final))
	fmt.Fprintf(&b, "<p class=\"meta\">Дата: %s. Проверено: %s.</p>\n", now.Format("2006-01-02"), html.EscapeString(strings.Join(r.Checked, ", ")))
	fmt.Fprintf(&b, "<p class=\"verdict %s\">%s</p>\n", className(r.Verdict()), html.EscapeString(r.verdictWords()))
	for _, g := range groups {
		list := r.byLevel(g.level)
		if len(list) == 0 {
			continue
		}
		fmt.Fprintf(&b, "<h2 class=\"%s\">%s</h2>\n", className(g.level), html.EscapeString(g.title))
		for _, f := range list {
			fmt.Fprintf(&b, "<section class=\"%s\"><h3><span class=\"area\">%s</span> %s</h3>\n", className(f.Level), html.EscapeString(f.Area), html.EscapeString(f.Text))
			if f.Why != "" {
				fmt.Fprintf(&b, "<p><b>Почему это важно.</b> %s.</p>\n", html.EscapeString(strings.TrimRight(f.Why, ".")))
			}
			if f.Fix != "" {
				fmt.Fprintf(&b, "<p><b>Что сделать.</b> <code>%s</code></p>\n", html.EscapeString(f.Fix))
			}
			b.WriteString("</section>\n")
		}
	}
	if len(r.Findings) == 0 {
		b.WriteString("<p>Замечаний нет.</p>\n")
	}
	b.WriteString("<p class=\"foot\">Отчёт собран утилитой tim: только открытые данные и обычные запросы к сайту.</p></main></body></html>\n")
	return b.String()
}

func (r *Report) byLevel(l level.Level) []Finding {
	var out []Finding
	for _, f := range r.Findings {
		if f.Level == l {
			out = append(out, f)
		}
	}
	return out
}

func className(l level.Level) string {
	switch l {
	case level.Bad:
		return "bad"
	case level.Warn:
		return "warn"
	}
	return "info"
}
