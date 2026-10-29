package sites

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/TimurTurdyev/tim/internal/config"
	"github.com/TimurTurdyev/tim/internal/dnsq"
	"github.com/TimurTurdyev/tim/internal/inspect"
	"github.com/TimurTurdyev/tim/internal/netinfo"
	"github.com/TimurTurdyev/tim/internal/par"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// printCert: сертификат одного хоста подробно - кому, кем, имена, сроки, цепочка.
// certJSON: сертификат для --json - то, что печатает tim cert, без самих байтов цепочки.
type certJSON struct {
	Host      string    `json:"host"`
	Port      string    `json:"port"`
	Subject   string    `json:"subject,omitempty"`
	Issuer    string    `json:"issuer,omitempty"`
	Names     []string  `json:"names,omitempty"`
	NotBefore time.Time `json:"not_before,omitzero"`
	NotAfter  time.Time `json:"not_after,omitzero"`
	DaysLeft  int       `json:"days_left"`
	TLS       string    `json:"tls,omitempty"`
	Chain     []string  `json:"chain,omitempty"`
	Verified  bool      `json:"verified"`
	Error     string    `json:"error,omitempty"`
}

func certJSONOf(host, port string) certJSON {
	c := certJSON{Host: host, Port: port}
	info, err := inspect.Cert(host, port)
	if err != nil {
		c.Error = err.Error()
		return c
	}
	leaf := info.Leaf()
	c.Subject, c.Issuer, c.Names = leaf.Subject.CommonName, leaf.Issuer.CommonName, leaf.DNSNames
	c.NotBefore, c.NotAfter, c.DaysLeft = leaf.NotBefore, leaf.NotAfter, ui.DaysUntil(leaf.NotAfter, time.Now())
	c.TLS, c.Verified = info.Version, info.VerifyErr == nil
	for _, x := range info.Chain[1:] {
		c.Chain = append(c.Chain, x.Subject.CommonName)
	}
	if info.VerifyErr != nil {
		c.Error = info.VerifyErr.Error()
	}
	return c
}

func printCert(arg string, asJSON bool) error {
	host, port := netinfo.HostPort(arg, "443")
	if asJSON {
		return ui.JSON(certJSONOf(host, port))
	}
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
}

// printExpiries: сроки сертификатов списком; без доменов - сайты проектов из реестра.
func printExpiries(cfg *config.Config, hosts []string, asJSON bool) error {
	if len(hosts) == 0 {
		hosts = Domains(cfg)
	}
	if len(hosts) == 0 {
		return ui.Errorf("доменов нет: tim cert <домен> или поле url у проектов (tim proj add --url)")
	}
	if asJSON {
		return ui.JSON(par.Map(hosts, par.Limit, func(h string) certJSON {
			host, port := netinfo.HostPort(h, "443")
			return certJSONOf(host, port)
		}))
	}
	rows := Expiries(hosts)
	bad := 0
	for _, r := range rows {
		if r.Err != nil && r.Expires.IsZero() {
			bad++
			ui.Println(ui.Bad(ui.Pad(r.Host, 30)) + " " + ui.Dim(r.Err.Error()))
			continue
		}
		line := ui.Pad(r.Host, 30) + " " + ui.DaysLeft(r.Expires) + "  " + ui.Dim(r.Issuer)
		if r.Err != nil || time.Until(r.Expires) < 14*24*time.Hour {
			bad++
		}
		if r.Err != nil {
			line += "  " + ui.Bad(ui.ShortErr(r.Err))
		}
		ui.Println(line)
	}
	if bad > 0 {
		return ui.Verdictf("требуют внимания: %d из %d", bad, len(rows))
	}
	return nil
}

// Domains: хосты сайтов проектов из реестра - у кого есть поле url.
func Domains(cfg *config.Config) []string {
	var out []string
	for _, p := range cfg.Projects {
		if h, _ := netinfo.HostPort(p.URL, ""); h != "" {
			out = append(out, h)
		}
	}
	return out
}

func printDNS(arg, server string, asJSON bool) error {
	host, _ := netinfo.HostPort(arg, "")
	recs := inspect.DNS(host, server)
	if len(recs) == 0 {
		return ui.Errorf("%s: записей нет", host)
	}
	if asJSON {
		out := struct {
			Host    string           `json:"host"`
			Records []inspect.Record `json:"records"`
			CAA     []string         `json:"caa,omitempty"`
			DNSSEC  *bool            `json:"dnssec,omitempty"`
		}{Host: host, Records: recs}
		if caa, err := dnsq.CAA(host); err == nil {
			out.CAA = caa
		}
		if signed, err := dnsq.Signed(host); err == nil {
			out.DNSSEC = &signed
		}
		return ui.JSON(out)
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
}

// flushDNS: на macOS - dscacheutil и mDNSResponder, на Linux кэш держит systemd-resolved или nscd;
// если ни того, ни другого, кэша в системе нет.
func flushDNS() error {
	var err error
	switch {
	case runtime.GOOS != "linux":
		if err = runner.Sudo("dscacheutil", "-flushcache"); err == nil {
			err = runner.Sudo("killall", "-HUP", "mDNSResponder")
		}
	case runner.Has("resolvectl"):
		err = runner.Sudo("resolvectl", "flush-caches")
	case runner.Has("nscd"):
		err = runner.Sudo("nscd", "-i", "hosts")
	default:
		ui.Println(ui.Dim("ни systemd-resolved, ни nscd: системного DNS-кэша нет, сбрасывать нечего"))
		return nil
	}
	if err != nil {
		return err
	}
	ui.Println(ui.Label("DNS-кэш сброшен"))
	return nil
}

func printDNSServers() error {
	servers := netinfo.DNSServers()
	if len(servers) == 0 {
		return ui.Errorf("DNS-серверы системы не нашлись")
	}
	for _, s := range servers {
		ui.Println(s)
	}
	return nil
}

// printSite: что за сайт - адреса, регистрация, сертификат, ответ HTTP.
func printSite(arg string, asJSON bool) error {
	host, _ := netinfo.HostPort(arg, "")
	target := arg
	if !strings.Contains(target, "://") {
		target = "https://" + host
	}
	r := Collect(host, target)
	if asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(r)
	}
	ui.Println(ui.Info(r.Host))
	if len(r.IPs) > 0 {
		line := ui.Label("ip       ") + strings.Join(r.IPs, " ")
		if g := r.Geo; g != nil {
			line += ui.Dim(fmt.Sprintf("  %s, %s, %s AS%d", g.Country, g.City, g.ISP, g.ASN))
		}
		ui.Println(line)
	}
	if d := r.Domain; d != nil {
		printRegistration(d)
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
	return nil
}

func printRegistration(d *inspect.Domain) {
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
