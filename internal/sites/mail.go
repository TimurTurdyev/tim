package sites

import (
	"sort"
	"strings"

	"github.com/TimurTurdyev/tim/internal/config"
	"github.com/TimurTurdyev/tim/internal/inspect"
	"github.com/TimurTurdyev/tim/internal/level"
	"github.com/TimurTurdyev/tim/internal/netinfo"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// printMail: MX, SPF, DKIM, DMARC домена, что с ними не так и какие записи добавить.
func printMail(arg string, selectors []string) error {
	domain, _ := netinfo.HostPort(arg, "")
	m := inspect.MailCheck(domain, append(append([]string{}, selectors...), inspect.DKIMSelectors...))
	bad := 0
	ui.Println(ui.Info("MX"))
	switch {
	case m.NullMX:
		ui.Println("  " + ui.Label("ок ") + "null MX: домен явно объявил, что почту не принимает")
	case len(m.MX) == 0:
		bad++
		ui.Println("  " + ui.Bad("x  ") + "MX нет: почта на домен не приходит")
	}
	for _, mx := range m.MX {
		ui.Println("  " + mx)
	}
	ui.Println(ui.Info("SPF") + ui.Dim("  "+m.SPF))
	bad += printFindings(m.SPFF)
	printDKIM(m)
	ui.Println(ui.Info("DMARC") + ui.Dim("  "+m.DMARC))
	bad += printFindings(m.DMRF)
	printFixes(domain, m)
	verdict, reasons := m.Verdict()
	ui.Println()
	switch verdict {
	case level.Skip:
		ui.Println(ui.Dim("итог не проверено: " + strings.Join(reasons, ", ")))
	case level.OK:
		ui.Println(ui.Label("итог ") + "почта настроена верно")
	case level.Warn:
		ui.Println(ui.Warn("итог ") + "работает, но: " + strings.Join(reasons, ", "))
	default:
		ui.Println(ui.Bad("итог ") + "есть проблемы: " + strings.Join(reasons, ", "))
	}
	if bad > 0 {
		return ui.Errorf("ошибок: %d", bad)
	}
	return nil
}

func printDKIM(m *inspect.Mail) {
	ui.Println(ui.Info("DKIM"))
	if len(m.DKIM) == 0 {
		ui.Println("  " + ui.Warn("!  ") + "по частым селекторам не найден; свой: --selector <имя>")
	}
	names := make([]string, 0, len(m.DKIM))
	for s := range m.DKIM {
		names = append(names, s)
	}
	sort.Strings(names)
	for _, s := range names {
		if DKIMRevoked(m.DKIM[s]) {
			ui.Println("  " + ui.Warn("!  ") + s + "._domainkey  ключ отозван (пустой p=)")
			continue
		}
		rec := m.DKIM[s]
		if len(rec) > 70 {
			rec = rec[:70] + "..."
		}
		ui.Println("  " + ui.Label("ок ") + s + "._domainkey  " + ui.Dim(rec))
	}
}

func printFindings(fs []inspect.Finding) int {
	bad := 0
	for _, f := range fs {
		switch f.Level {
		case level.OK:
			ui.Println("  " + ui.Label("ок ") + f.Text)
		case level.Warn:
			ui.Println("  " + ui.Warn("!  ") + f.Text)
		case level.Bad:
			bad++
			ui.Println("  " + ui.Bad("x  ") + f.Text)
		default:
			ui.Println("  " + ui.Dim("-  "+f.Text))
		}
	}
	return bad
}

// printFixes печатает готовые записи для того, чего не хватает: чтобы не искать синтаксис в интернете.
func printFixes(domain string, m *inspect.Mail) {
	var lines []string
	if m.DMARC == "" && !m.NullMX {
		lines = append(lines, "_dmarc."+domain+`  TXT  "v=DMARC1; p=none; rua=mailto:dmarc@`+domain+`"`)
	}
	if m.SPF == "" && !m.NullMX {
		lines = append(lines, domain+`  TXT  "v=spf1 include:<сервер отправки> ~all"`)
	}
	if len(lines) == 0 {
		return
	}
	ui.Println()
	ui.Println(ui.Info("Что добавить в DNS"))
	for _, l := range lines {
		ui.Println("  " + l)
	}
	if m.DMARC == "" {
		ui.Println(ui.Dim("  p=none только собирает отчёты и ничего не ломает; через пару недель по отчётам"))
		ui.Println(ui.Dim("  переключают на p=quarantine, затем на p=reject"))
	}
}

// printMailSummary: почта всех доменов реестра одной таблицей.
func printMailSummary(cfg *config.Config) error {
	domains := Domains(cfg)
	if len(domains) == 0 {
		return ui.Errorf("ни у одного проекта нет поля url: tim proj add --url")
	}
	sort.Strings(domains)
	rows := MailVerdicts(domains)
	bad := 0
	ui.Println(ui.Dim(ui.Pad("ДОМЕН", 30) + " ИТОГ"))
	for _, r := range rows {
		switch r.Level {
		case level.Skip:
			ui.Println(ui.Pad(r.Domain, 30) + " " + ui.Dim("не проверено: "+strings.Join(r.Missing, ", ")))
		case level.OK:
			ui.Println(ui.Label(ui.Pad(r.Domain, 30)) + " настроена верно")
		case level.Warn:
			bad++
			ui.Println(ui.Pad(r.Domain, 30) + " " + ui.Warn(strings.Join(r.Missing, ", ")))
		default:
			bad++
			ui.Println(ui.Pad(r.Domain, 30) + " " + ui.Bad("проблемы: "+strings.Join(r.Missing, ", ")))
		}
	}
	ui.Println(ui.Dim("подробно по домену: tim mail <домен>"))
	if bad > 0 {
		return ui.Errorf("требуют внимания: %d из %d", bad, len(rows))
	}
	return nil
}
