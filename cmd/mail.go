package cmd

import (
	"sort"
	"strings"
	"sync"

	"github.com/spf13/cobra"

	"github.com/TimurTurdyev/tim/internal/inspect"
	"github.com/TimurTurdyev/tim/internal/ui"
)

var mailSelectors []string

func printFindings(fs []inspect.Finding) int {
	bad := 0
	for _, f := range fs {
		switch f.Level {
		case "ok":
			ui.Println("  " + ui.Label("ок ") + f.Text)
		case "warn":
			ui.Println("  " + ui.Warn("!  ") + f.Text)
		case "bad":
			bad++
			ui.Println("  " + ui.Bad("x  ") + f.Text)
		default:
			ui.Println("  " + ui.Dim("-  "+f.Text))
		}
	}
	return bad
}

// fixes печатает готовые записи для того, чего не хватает: чтобы не искать синтаксис в интернете.
func fixes(domain string, m *inspect.Mail) {
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

var mailAll bool

// mailAll: сводка по всем доменам реестра одной таблицей.
func mailSummary() error {
	type row struct {
		domain, level string
		missing       []string
	}
	var domains []string
	for _, p := range cfg.Projects {
		if p.URL != "" {
			h, _ := hostPort(p.URL, "")
			domains = append(domains, h)
		}
	}
	if len(domains) == 0 {
		return ui.Errorf("ни у одного проекта нет поля url: tim proj add --url")
	}
	sort.Strings(domains)
	rows := make([]row, len(domains))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for i, d := range domains {
		wg.Go(func() {
			sem <- struct{}{}
			defer func() { <-sem }()
			m := inspect.MailCheck(d, inspect.DKIMSelectors)
			level, missing := m.Verdict()
			rows[i] = row{d, level, missing}
		})
	}
	wg.Wait()
	bad := 0
	ui.Println(ui.Dim(ui.Pad("ДОМЕН", 30) + " ИТОГ"))
	for _, r := range rows {
		switch r.level {
		case "ok":
			ui.Println(ui.Label(ui.Pad(r.domain, 30)) + " настроена верно")
		case "warn":
			bad++
			ui.Println(ui.Pad(r.domain, 30) + " " + ui.Warn(strings.Join(r.missing, ", ")))
		default:
			bad++
			ui.Println(ui.Pad(r.domain, 30) + " " + ui.Bad("проблемы: "+strings.Join(r.missing, ", ")))
		}
	}
	ui.Println(ui.Dim("подробно по домену: tim mail <домен>"))
	if bad > 0 {
		return ui.Errorf("требуют внимания: %d из %d", bad, len(rows))
	}
	return nil
}

var mailCmd = &cobra.Command{
	Use:   "mail [домен] [--all] [--selector s]",
	Short: "Почта домена: MX, SPF, DKIM, DMARC и что с ними не так",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if mailAll {
			return mailSummary()
		}
		if len(args) == 0 {
			return ui.Errorf("укажи домен: tim mail example.com (или --all по доменам реестра)")
		}
		domain, _ := hostPort(args[0], "")
		sels := append(append([]string{}, mailSelectors...), inspect.DKIMSelectors...)
		m := inspect.MailCheck(domain, sels)
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
			rec := m.DKIM[s]
			if len(rec) > 70 {
				rec = rec[:70] + "..."
			}
			if dkimRevoked(m.DKIM[s]) {
				ui.Println("  " + ui.Warn("!  ") + s + "._domainkey  ключ отозван (пустой p=)")
				continue
			}
			ui.Println("  " + ui.Label("ок ") + s + "._domainkey  " + ui.Dim(rec))
		}
		ui.Println(ui.Info("DMARC") + ui.Dim("  "+m.DMARC))
		bad += printFindings(m.DMRF)
		fixes(domain, m)
		level, reasons := m.Verdict()
		ui.Println()
		switch level {
		case "ok":
			ui.Println(ui.Label("итог ") + "почта настроена верно")
		case "warn":
			ui.Println(ui.Warn("итог ") + "работает, но: " + strings.Join(reasons, ", "))
		default:
			ui.Println(ui.Bad("итог ") + "есть проблемы: " + strings.Join(reasons, ", "))
		}
		if bad > 0 {
			return ui.Errorf("ошибок: %d", bad)
		}
		return nil
	},
}

// dkimRevoked: тег p= пустой - ключ отозван.
func dkimRevoked(rec string) bool {
	for _, part := range strings.Split(rec, ";") {
		k, v, ok := strings.Cut(strings.TrimSpace(part), "=")
		if ok && strings.TrimSpace(k) == "p" {
			return strings.TrimSpace(v) == ""
		}
	}
	return false
}

func init() {
	mailCmd.Flags().BoolVar(&mailAll, "all", false, "сводка по доменам реестра")
	mailCmd.Flags().StringSliceVar(&mailSelectors, "selector", nil, "свой DKIM-селектор (можно несколько)")
	root.AddCommand(mailCmd)
}
