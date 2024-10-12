package cmd

import (
	"encoding/json"
	"fmt"
	"maps"
	"net"
	"net/url"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/net/publicsuffix"

	"github.com/TimurTurdyev/tim/internal/inspect"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// hostPort: из "host", "host:port" или url достаёт хост и порт.
func hostPort(s, defPort string) (string, string) {
	if u, err := url.Parse(s); err == nil && u.Host != "" {
		s = u.Host
	}
	if h, p, err := net.SplitHostPort(s); err == nil {
		return h, p
	}
	return strings.TrimSuffix(s, "/"), defPort
}

func daysLeft(t time.Time) string {
	d := int(time.Until(t).Hours() / 24)
	s := fmt.Sprintf("%s, осталось %d дн", t.Format("2006-01-02"), d)
	switch {
	case d < 0:
		return ui.Bad(fmt.Sprintf("%s, истёк %d дн назад", t.Format("2006-01-02"), -d))
	case d < 14:
		return ui.Warn(s)
	}
	return s
}

var certCmd = &cobra.Command{
	Use:   "cert <host[:port]>",
	Short: "TLS-сертификат: кому выдан, кем, SAN, срок, цепочка",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		host, port := hostPort(args[0], "443")
		info, err := inspect.Cert(host, port)
		if err != nil {
			return ui.Errorf("%s:%s: %v", host, port, err)
		}
		leaf := info.Leaf()
		ui.Println(ui.Label("кому     ") + leaf.Subject.CommonName)
		ui.Println(ui.Label("кем      ") + leaf.Issuer.CommonName + ui.Dim("  "+strings.Join(leaf.Issuer.Organization, ", ")))
		ui.Println(ui.Label("имена    ") + strings.Join(leaf.DNSNames, " "))
		ui.Println(ui.Label("с        ") + leaf.NotBefore.Format("2006-01-02"))
		ui.Println(ui.Label("до       ") + daysLeft(leaf.NotAfter))
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
	Short: "Записи A, AAAA, CNAME, MX, NS, TXT",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		host, _ := hostPort(args[0], "")
		recs := inspect.DNS(host, dnsServer)
		if len(recs) == 0 {
			return ui.Errorf("%s: записей нет", host)
		}
		for _, r := range recs {
			ui.Println(ui.Label(ui.Pad(r.Type, 6)) + r.Value)
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

type siteReport struct {
	Host   string            `json:"host"`
	IPs    []string          `json:"ips,omitempty"`
	Geo    *inspect.Geo      `json:"geo,omitempty"`
	Domain *inspect.Domain   `json:"domain,omitempty"`
	TLS    *siteTLS          `json:"tls,omitempty"`
	HTTP   *inspect.Page     `json:"http,omitempty"`
	Errors map[string]string `json:"errors,omitempty"`
}

type siteTLS struct {
	Issuer   string    `json:"issuer"`
	Expires  time.Time `json:"expires"`
	Verified bool      `json:"verified"`
}

var siteJSON bool

var siteCmd = &cobra.Command{
	Use:   "site <домен|url>",
	Short: "Что за сайт: IP и где он, регистрация домена, сертификат, HTTP, CMS",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		host, _ := hostPort(args[0], "")
		target := args[0]
		if !strings.Contains(target, "://") {
			target = "https://" + host
		}
		r := collectSite(host, target)
		if siteJSON {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(r)
		}
		printSite(r)
		return nil
	},
}

func collectSite(host, target string) *siteReport {
	r := &siteReport{Host: host, Errors: map[string]string{}}
	var mu sync.Mutex
	fail := func(k string, err error) {
		mu.Lock()
		r.Errors[k] = err.Error()
		mu.Unlock()
	}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		ips, err := net.LookupHost(host)
		if err != nil {
			fail("dns", err)
			return
		}
		r.IPs = ips
		if g, err := inspect.IPGeo(ips[0]); err == nil {
			r.Geo = g
		} else {
			fail("geo", err)
		}
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		if d, err := inspect.Registration(registrable(host)); err == nil {
			r.Domain = d
		} else {
			fail("domain", err)
		}
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		info, err := inspect.Cert(host, "443")
		if err != nil {
			fail("tls", err)
			return
		}
		r.TLS = &siteTLS{Issuer: info.Leaf().Issuer.CommonName, Expires: info.Leaf().NotAfter, Verified: info.VerifyErr == nil}
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		if p, err := inspect.HTTP(target); err == nil {
			r.HTTP = p
		} else {
			fail("http", err)
		}
	}()
	wg.Wait()
	return r
}

// registrable: домен, который регистрируют (по таблице публичных суффиксов), для запроса RDAP и whois.
func registrable(host string) string {
	host = strings.TrimSuffix(host, ".")
	if d, err := publicsuffix.EffectiveTLDPlusOne(host); err == nil {
		return d
	}
	parts := strings.Split(host, ".")
	if len(parts) <= 2 {
		return host
	}
	return strings.Join(parts[len(parts)-2:], ".")
}

func printSite(r *siteReport) {
	ui.Println(ui.Info(r.Host))
	if len(r.IPs) > 0 {
		line := ui.Label("ip       ") + strings.Join(r.IPs, " ")
		if g := r.Geo; g != nil {
			line += ui.Dim(fmt.Sprintf("  %s, %s, %s AS%d", g.Country, g.City, g.ISP, g.ASN))
		}
		ui.Println(line)
	}
	if d := r.Domain; d != nil {
		ui.Println(ui.Label("домен    ") + dashIfEmpty(d.Registrar) + ui.Dim("  "+d.Source))
		if !d.Created.IsZero() {
			ui.Println(ui.Label("создан   ") + d.Created.Format("2006-01-02"))
		}
		if !d.Expires.IsZero() {
			ui.Println(ui.Label("оплачен  ") + daysLeft(d.Expires))
		}
		if len(d.Nameservers) > 0 {
			ui.Println(ui.Label("ns       ") + strings.Join(d.Nameservers, " "))
		}
	}
	if t := r.TLS; t != nil {
		line := ui.Label("tls      ") + t.Issuer + ", до " + daysLeft(t.Expires)
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
		ui.Println(ui.Label("cms      ") + dashIfEmpty(p.CMS))
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
