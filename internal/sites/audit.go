package sites

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/TimurTurdyev/tim/internal/audit"
	"github.com/TimurTurdyev/tim/internal/ui"
	"github.com/TimurTurdyev/tim/internal/view"
	"github.com/TimurTurdyev/tim/sitecheck"
)

// auditOptions: как отдать отчёт - на экран, JSON, файлом для клиента.
type auditOptions struct {
	json, md, html bool
	out            string
}

// runAudit: проверка сайта снаружи; цель - домен или url. Пути вроде /.env и порты баз tim проверяет
// всегда: tim запускают на своих сайтах или с согласия владельца.
func runAudit(arg string, o auditOptions) error {
	target, err := sitecheck.Target(arg)
	if err != nil {
		return ui.Errorf("%v: tim audit example.ru", err)
	}
	if !o.json {
		ui.Println(ui.Dim("проверяю " + target + ": главная, заголовки, куки, " + fmt.Sprint(len(audit.ProbePaths())) + " путей, разметка, dns"))
	}
	r, err := sitecheck.Check(target, sitecheck.Options{Intrusive: true})
	if err != nil {
		return ui.Errorf("%s: %v", target, err)
	}
	if o.md || o.html {
		view.Print(r)
		return writeReport(r.Data.(*audit.Report), o)
	}
	return view.Show(r, o.json)
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
