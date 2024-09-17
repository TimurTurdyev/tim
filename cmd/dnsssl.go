package cmd

import (
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"

	"github.com/TimurTurdyev/tim/internal/inspect"
	"github.com/TimurTurdyev/tim/internal/netinfo"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/ui"
)

var dnsFlushCmd = &cobra.Command{
	Use:   "flush",
	Short: "Сбросить DNS-кэш macOS (sudo)",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := runner.Sudo("dscacheutil", "-flushcache"); err != nil {
			return err
		}
		if err := runner.Sudo("killall", "-HUP", "mDNSResponder"); err != nil {
			return err
		}
		ui.Println(ui.Label("DNS-кэш сброшен"))
		return nil
	},
}

var dnsServersCmd = &cobra.Command{
	Use:   "servers",
	Short: "DNS-серверы основного резолвера",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		servers := netinfo.DNSServers()
		if len(servers) == 0 {
			return ui.Errorf("основной резолвер не найден в scutil --dns")
		}
		for _, s := range servers {
			ui.Println(s)
		}
		return nil
	},
}

type sslRow struct {
	host    string
	expires time.Time
	issuer  string
	err     error
}

var sslCmd = &cobra.Command{
	Use:   "ssl [домен...]",
	Short: "Сроки сертификатов; без аргументов - домены из поля url проектов",
	RunE: func(cmd *cobra.Command, args []string) error {
		hosts := args
		if len(hosts) == 0 {
			for _, p := range cfg.Projects {
				if p.URL != "" {
					h, _ := hostPort(p.URL, "")
					hosts = append(hosts, h)
				}
			}
		}
		if len(hosts) == 0 {
			return ui.Errorf("доменов нет: tim ssl <домен> или поле url у проектов (tim proj add --url)")
		}
		rows := make([]sslRow, len(hosts))
		var wg sync.WaitGroup
		for i, h := range hosts {
			wg.Add(1)
			go func() {
				defer wg.Done()
				host, port := hostPort(h, "443")
				rows[i].host = host
				info, err := inspect.Cert(host, port)
				if err != nil {
					rows[i].err = err
					return
				}
				rows[i].expires, rows[i].issuer = info.Leaf().NotAfter, info.Leaf().Issuer.CommonName
				if info.VerifyErr != nil {
					rows[i].err = info.VerifyErr
				}
			}()
		}
		wg.Wait()
		sort.SliceStable(rows, func(a, b int) bool {
			if (rows[a].err != nil) != (rows[b].err != nil) {
				return rows[a].err != nil
			}
			return rows[a].expires.Before(rows[b].expires)
		})
		bad := 0
		for _, r := range rows {
			if r.err != nil && r.expires.IsZero() {
				bad++
				ui.Println(ui.Bad(ui.Pad(r.host, 30)) + " " + ui.Dim(r.err.Error()))
				continue
			}
			line := ui.Pad(r.host, 30) + " " + daysLeft(r.expires) + "  " + ui.Dim(r.issuer)
			if r.err != nil || time.Until(r.expires) < 14*24*time.Hour {
				bad++
			}
			if r.err != nil {
				line += "  " + ui.Bad(shortErr(r.err))
			}
			ui.Println(line)
		}
		if bad > 0 {
			return ui.Errorf("требуют внимания: %d из %d", bad, len(rows))
		}
		return nil
	},
}

func shortErr(err error) string {
	s := err.Error()
	if i := strings.LastIndex(s, ": "); i >= 0 {
		s = s[i+2:]
	}
	return s
}

func init() {
	dnsCmd.AddCommand(dnsFlushCmd, dnsServersCmd)
	root.AddCommand(sslCmd)
}
