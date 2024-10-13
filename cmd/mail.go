package cmd

import (
	"sort"
	"strings"

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

var mailCmd = &cobra.Command{
	Use:   "mail <домен> [--selector s]",
	Short: "Почта домена: MX, SPF, DKIM, DMARC и что с ними не так",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
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
	mailCmd.Flags().StringSliceVar(&mailSelectors, "selector", nil, "свой DKIM-селектор (можно несколько)")
	root.AddCommand(mailCmd)
}
