package sites

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/TimurTurdyev/tim/internal/app"
	"github.com/TimurTurdyev/tim/internal/audit"
	"github.com/TimurTurdyev/tim/internal/level"
	"github.com/TimurTurdyev/tim/internal/ui"
	"github.com/TimurTurdyev/tim/internal/view"
	"github.com/TimurTurdyev/tim/report"
)

// auditOptions: как отдать отчёт - на экран, JSON, файлом для клиента.
type auditOptions struct {
	json, md, html bool
	out            string
}

// runAudit: проверка сайта снаружи; цель - домен или url.
func runAudit(env *app.Env, arg string, o auditOptions) error {
	target, err := auditTarget(env, arg)
	if err != nil {
		return err
	}
	if !o.json {
		ui.Println(ui.Dim("проверяю " + target + ": главная, заголовки, куки, " + fmt.Sprint(len(audit.ProbePaths())) + " путей, разметка, dns"))
	}
	r, err := audit.Run(target)
	if err != nil {
		return ui.Errorf("%s: %v", target, err)
	}
	if o.md || o.html {
		view.Print(auditReport(r))
		return writeReport(r, o)
	}
	return view.Show(auditReport(r), o.json)
}

// writeReport: отчёт для клиента файлом рядом, имя - сайт и дата, если не задано --out.
func writeReport(r *audit.Report, o auditOptions) error {
	now := time.Now()
	ext, text := "md", r.Markdown(now)
	if o.html {
		ext, text = "html", r.HTML(now)
	}
	path := o.out
	if path == "" {
		host := strings.NewReplacer("https://", "", "http://", "", "/", "_", ":", "_").Replace(r.Final)
		path = "audit-" + strings.TrimRight(host, "_") + "-" + now.Format("2006-01-02") + "." + ext
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		return ui.Errorf("отчёт не записан: %v", err)
	}
	ui.Println(ui.Label("отчёт: ") + path)
	return nil
}

// auditTarget: адрес сайта по аргументу. Домен без схемы - https.
func auditTarget(env *app.Env, arg string) (string, error) {
	switch {
	case strings.Contains(arg, "://"):
		return arg, nil
	case strings.Contains(arg, "."):
		return "https://" + arg, nil
	case arg == "":
		return "", ui.Errorf("укажи сайт: tim audit example.ru")
	}
	return "", ui.Errorf("%s - не домен: tim audit example.ru", arg)
}

// auditReport: вердикт рядом с адресом, находки - худшие сверху, у каждой почему и что сделать.
func auditReport(r *audit.Report) report.Report {
	return report.Report{Schema: "tim.audit/1", Title: r.Final, Data: r,
		Verdict: report.Verdict{Level: r.Verdict(), Text: verdictText(r)},
		Blocks:  []report.Block{report.Findings(r.Findings), report.Note("проверено: " + strings.Join(r.Checked, ", "))}}
}

func verdictText(r *audit.Report) string {
	var bad, warn int
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
		return fmt.Sprintf("плохо: %d %s", bad, ui.Plural(bad, "серьёзная находка", "серьёзные находки", "серьёзных находок"))
	case warn > 0:
		return fmt.Sprintf("есть что поправить: %d", warn)
	}
	return "в порядке"
}
