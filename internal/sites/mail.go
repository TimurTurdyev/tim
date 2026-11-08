package sites

import (
	"errors"
	"fmt"
	"net"
	"sort"
	"strings"

	"github.com/TimurTurdyev/tim/internal/config"
	"github.com/TimurTurdyev/tim/internal/inspect"
	"github.com/TimurTurdyev/tim/internal/level"
	"github.com/TimurTurdyev/tim/internal/netinfo"
	"github.com/TimurTurdyev/tim/internal/ui"
	"github.com/TimurTurdyev/tim/internal/view"
	"github.com/TimurTurdyev/tim/report"
)

// printMail: MX, SPF, DKIM, DMARC домена, что с ними не так и какие записи добавить.
func printMail(arg string, selectors []string, asJSON bool) error {
	domain, _ := netinfo.HostPort(arg, "")
	m := inspect.MailCheck(domain, append(append([]string{}, selectors...), inspect.DKIMSelectors...))
	if asJSON {
		verdict, reasons := m.Verdict()
		return ui.JSON(struct {
			Domain  string        `json:"domain"`
			Mail    *inspect.Mail `json:"mail"`
			Verdict level.Level   `json:"verdict"`
			Reasons []string      `json:"reasons,omitempty"`
		}{domain, m, verdict, reasons})
	}
	if noMail(m) {
		if _, err := net.LookupHost(domain); err != nil && len(m.DMARCOther) == 0 {
			if dnsErr, ok := errors.AsType[*net.DNSError](err); ok && dnsErr.IsNotFound {
				return ui.Errorf("%s: такого имени в DNS нет - проверь, нет ли опечатки", domain)
			}
		}
		printMailGuide(domain)
		return ui.Verdictf("почты нет")
	}
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
	if lvl, text := mxBlacklist(m.MX); lvl != level.Skip {
		if lvl == level.Bad {
			bad++
		}
		ui.Println("  " + level.Lamp(lvl, "чёрные списки: "+text))
	}
	ui.Println(ui.Info("SPF") + ui.Dim("  "+m.SPF))
	bad += printFindings(m.SPFF)
	printDKIM(m)
	ui.Println(ui.Info("DMARC") + ui.Dim("  "+m.DMARC))
	bad += printFindings(m.DMRF)
	if m.DMARC == "" && len(m.DMARCOther) > 0 {
		ui.Println("  " + ui.Warn("!  ") + "на _dmarc." + domain + " лежат другие TXT: " + strings.Join(cut(m.DMARCOther, 2), ", ") +
			ui.Dim(" - DMARC среди них нет, их положили не туда"))
	}
	printMailFixes(mailFixes(domain, m))
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
	hijackNote()
	if bad > 0 {
		return ui.Verdictf("ошибок: %d", bad)
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
	revoked := 0
	for _, s := range names {
		if DKIMRevoked(m.DKIM[s]) {
			revoked++
		}
	}
	// Закрытый от почты домен отзывает ключи на всех селекторах сразу: это одна мысль, а не десяток строк.
	if revoked == len(names) && revoked > 2 {
		ui.Println("  " + ui.Label("ок ") + fmt.Sprintf("ключи на %d селекторах отозваны (пустой p=)", revoked) +
			ui.Dim(" - так закрывают домен от подписанных подделок"))
		return
	}
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

// printMailSummary: почта всех доменов из списка сайтов одной таблицей.
func printMailSummary(cfg *config.Config) error {
	domains := roundDomains(siteHosts(cfg, nil))
	if len(domains) == 0 {
		return ui.Errorf("сайтов нет: добавь их в конфиг - sites = [\"example.com\"]")
	}
	sort.Strings(domains)
	rows := MailVerdicts(domains)
	type mailRow struct {
		Domain  string       `json:"domain"`
		Level   report.Level `json:"level"`
		Missing []string     `json:"missing,omitempty"`
	}
	data := make([]mailRow, 0, len(rows))
	t := report.Table{Columns: []report.Column{{Title: "ДОМЕН", Style: report.Key}, {Title: "ИТОГ"}}}
	bad := 0
	for _, r := range rows {
		data = append(data, mailRow{r.Domain, r.Level, r.Missing})
		missing := strings.Join(r.Missing, ", ")
		verdict := report.Mark{Text: "проблемы: " + missing, Level: report.Bad}
		switch r.Level {
		case level.Skip:
			verdict = report.Mark{Text: "не проверено: " + missing, Level: report.Info}
		case level.OK:
			verdict = report.Mark{Text: "настроена верно", Level: report.OK}
		case level.Warn:
			verdict = report.Mark{Text: missing, Level: report.Warn}
		}
		if r.Level >= level.Warn {
			bad++
		}
		t.Rows = append(t.Rows, []any{r.Domain, verdict})
	}
	err := view.Show(report.Report{Schema: "tim.mail.all/1", Data: data, Blocks: []report.Block{t,
		report.Note("подробно по домену: tim mail <домен>")}}, view.AsJSON)
	if err == nil && bad > 0 {
		return ui.Verdictf("требуют внимания: %d из %d", bad, len(rows))
	}
	return err
}
