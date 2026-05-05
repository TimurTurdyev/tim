package cmd

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/TimurTurdyev/tim/internal/dnsq"
	"github.com/TimurTurdyev/tim/internal/inspect"
	"github.com/TimurTurdyev/tim/internal/netinfo"
	"github.com/TimurTurdyev/tim/internal/sites"
	"github.com/TimurTurdyev/tim/internal/ui"
)

var certCmd = &cobra.Command{
	Use:   "cert [host[:port]...]",
	Short: "TLS-сертификат: один домен - подробно, несколько или без аргументов - сроки списком",
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) != 1 {
			return certExpiries(args)
		}
		host, port := netinfo.HostPort(args[0], "443")
		info, err := inspect.Cert(host, port)
		if err != nil {
			return ui.Errorf("%s:%s: %v", host, port, err)
		}
		leaf := info.Leaf()
		ui.Println(ui.Label("кому     ") + leaf.Subject.CommonName)
		ui.Println(ui.Label("кем      ") + leaf.Issuer.CommonName + ui.Dim("  "+strings.Join(leaf.Issuer.Organization, ", ")))
		ui.Println(ui.Label("имена    ") + strings.Join(leaf.DNSNames, " "))
		ui.Println(ui.Label("с        ") + leaf.NotBefore.Format("2006-01-02"))
		ui.Println(ui.Label("до       ") + ui.DaysLeft(leaf.NotAfter))
		ui.Println(ui.Label("tls      ") + info.Version)
		for i, c := range info.Chain[1:] {
			ui.Println(ui.Dim(fmt.Sprintf("цепочка %d  %s", i+1, c.Subject.CommonName)))
		}
		if info.VerifyErr != nil {
			ui.Println(ui.Bad("проверка не прошла: " + info.VerifyErr.Error()))
		} else {
			ui.Println(ui.Info("цепочка проверена"))
		}
		return nil
	},
}

var dnsServer string

var dnsCmd = &cobra.Command{
	Use:   "dns <host> [--server 1.1.1.1]",
	Short: "Записи A, AAAA, CNAME, MX, NS, TXT, CAA и подписана ли зона (DNSSEC)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		host, _ := netinfo.HostPort(args[0], "")
		recs := inspect.DNS(host, dnsServer)
		if len(recs) == 0 {
			return ui.Errorf("%s: записей нет", host)
		}
		for _, r := range recs {
			ui.Println(ui.Label(ui.Pad(r.Type, 6)) + r.Value)
		}
		// CAA и DNSSEC спрашиваем у проверяющего резолвера: системный за VPN может не ставить бит AD.
		if caa, err := dnsq.CAA(host); err == nil {
			for _, c := range caa {
				ui.Println(ui.Label(ui.Pad("CAA", 6)) + c)
			}
		}
		if signed, err := dnsq.Signed(host); err == nil {
			state := ui.Dim("не подписана")
			if signed {
				state = "подписана, подпись проверена"
			}
			ui.Println(ui.Label(ui.Pad("DNSSEC", 6)) + " " + state)
		}
		return nil
	},
}

var portCmd = &cobra.Command{
	Use:   "port",
	Short: "Порты: проверка доступности",
}

var portCheckCmd = &cobra.Command{
	Use:   "check <host:port...>",
	Short: "Открыт ли TCP-порт и за сколько",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		failed := 0
		for _, a := range args {
			d, err := inspect.Dial(a)
			if err != nil {
				failed++
				ui.Println(ui.Bad(ui.Pad(a, 28)) + " " + ui.Dim(err.Error()))
				continue
			}
			ui.Println(ui.Label(ui.Pad(a, 28)) + fmt.Sprintf(" открыт, %d мс", d.Milliseconds()))
		}
		if failed > 0 {
			return ui.Errorf("закрыто: %d из %d", failed, len(args))
		}
		return nil
	},
}

var siteJSON bool

var siteCmd = &cobra.Command{
	Use:   "site <домен|url>",
	Short: "Что за сайт: IP и где он, регистрация домена, сертификат, HTTP, CMS",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		host, _ := netinfo.HostPort(args[0], "")
		target := args[0]
		if !strings.Contains(target, "://") {
			target = "https://" + host
		}
		r := sites.Collect(host, target)
		if siteJSON {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(r)
		}
		printSite(r)
		return nil
	},
}

func printSite(r *sites.Report) {
	ui.Println(ui.Info(r.Host))
	if len(r.IPs) > 0 {
		line := ui.Label("ip       ") + strings.Join(r.IPs, " ")
		if g := r.Geo; g != nil {
			line += ui.Dim(fmt.Sprintf("  %s, %s, %s AS%d", g.Country, g.City, g.ISP, g.ASN))
		}
		ui.Println(line)
	}
	if d := r.Domain; d != nil {
		ui.Println(ui.Label("домен    ") + ui.Dash(d.Registrar) + ui.Dim("  "+d.Source))
		if !d.Created.IsZero() {
			ui.Println(ui.Label("создан   ") + d.Created.Format("2006-01-02"))
		}
		if !d.Expires.IsZero() {
			ui.Println(ui.Label("оплачен  ") + ui.DaysLeft(d.Expires))
		}
		if len(d.Nameservers) > 0 {
			ui.Println(ui.Label("ns       ") + strings.Join(d.Nameservers, " "))
		}
	}
	if t := r.TLS; t != nil {
		line := ui.Label("tls      ") + t.Issuer + ", до " + ui.DaysLeft(t.Expires)
		if !t.Verified {
			line += "  " + ui.Bad("не проверен")
		}
		ui.Println(line)
	}
	if p := r.HTTP; p != nil {
		ui.Println(ui.Label("http     ") + fmt.Sprintf("%d %s", p.Status, p.Final))
		for _, rd := range p.Redirects {
			ui.Println(ui.Dim("  через " + rd))
		}
		if p.Server != "" || p.PoweredBy != "" {
			ui.Println(ui.Label("сервер   ") + strings.TrimSpace(p.Server+" "+p.PoweredBy))
		}
		ui.Println(ui.Label("cms      ") + ui.Dash(p.CMS))
	}
	for _, k := range slices.Sorted(maps.Keys(r.Errors)) {
		ui.Println(ui.Warn(ui.Pad(k, 9)) + ui.Dim(r.Errors[k]))
	}
}

func init() {
	dnsCmd.Flags().StringVar(&dnsServer, "server", "", "резолвер, например 1.1.1.1")
	siteCmd.Flags().BoolVar(&siteJSON, "json", false, "вывод в JSON")
	portCmd.AddCommand(portCheckCmd)
	root.AddCommand(certCmd, dnsCmd, portCmd, siteCmd)
}
