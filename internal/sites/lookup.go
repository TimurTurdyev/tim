package sites

import (
	"errors"
	"fmt"
	"maps"
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
	"github.com/TimurTurdyev/tim/internal/view"
	"github.com/TimurTurdyev/tim/report"
)

// hijackText: почему записи могут разойтись с тем, что показывает dig на этой же машине.
const hijackText = "DNS на этой машине перехвачен (VPN или фильтр): записи взяты через DNS-over-HTTPS, мимо его кэша"

// printCert: сертификат одного хоста подробно - кому, кем, имена, сроки, цепочка.
// certJSON: сертификат для --json - то, что печатает tim cert, без самих байтов цепочки.
type certJSON struct {
	Host      string    `json:"host"`
	Port      string    `json:"port"`
	Subject   string    `json:"subject,omitempty"`
	Issuer    string    `json:"issuer,omitempty"`
	IssuerOrg string    `json:"issuer_org,omitempty"`
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
	c.IssuerOrg = strings.Join(leaf.Issuer.Organization, ", ")
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

func hijackNote() {
	if dnsq.Hijacked() {
		ui.Println(ui.Dim(hijackText))
	}
}

// dnsData: записи для --json; CAA и DNSSEC - если резолвер ответил.
type dnsData struct {
	Host    string           `json:"host"`
	Records []inspect.Record `json:"records"`
	CAA     []string         `json:"caa,omitempty"`
	DNSSEC  *bool            `json:"dnssec,omitempty"`
}

func printDNS(arg, server string, asJSON bool) error {
	host, _ := netinfo.HostPort(arg, "")
	recs := inspect.DNS(host, server)
	if len(recs) == 0 {
		return ui.Errorf("%s: записей нет", host)
	}
	d := dnsData{Host: host, Records: recs}
	// CAA и DNSSEC спрашиваем у проверяющего резолвера: системный за VPN может не ставить бит AD.
	if caa, err := dnsq.CAA(host); err == nil {
		d.CAA = caa
	}
	if signed, err := dnsq.Signed(host); err == nil {
		d.DNSSEC = &signed
	}
	return view.Show(dnsReport(d, server == "" && dnsq.Hijacked()), asJSON)
}

func dnsReport(d dnsData, hijacked bool) report.Report {
	var rows report.Facts
	for _, r := range d.Records {
		rows = append(rows, report.Fact{Key: r.Type, Value: r.Value})
	}
	for _, c := range d.CAA {
		rows = append(rows, report.Fact{Key: "CAA", Value: c})
	}
	if d.DNSSEC != nil {
		state := report.Fact{Key: "DNSSEC", Value: "подписана, подпись проверена"}
		if !*d.DNSSEC {
			state.Value, state.Level = "не подписана", report.Info
		}
		rows = append(rows, state)
	}
	r := report.Report{Schema: "tim.dns/1", Title: d.Host, Data: d, Blocks: []report.Block{rows}}
	if hijacked {
		r.Blocks = append(r.Blocks, report.Note(hijackText))
	}
	return r
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
	if view.AsJSON {
		return view.JSON(report.Report{Data: servers})
	}
	for _, s := range servers {
		ui.Println(s)
	}
	return nil
}

// daysFact: срок строкой и уровнем: истёк - плохо, меньше двух недель - внимание.
func daysFact(key string, t time.Time) report.Fact {
	f := report.Fact{Key: key, Value: ui.StripANSI(ui.DaysLeft(t))}
	switch d := ui.DaysUntil(t, time.Now()); {
	case d <= 0:
		f.Level = report.Bad
	case d < 14:
		f.Level = report.Warn
	}
	return f
}

// printCert: сертификат одного хоста подробно - кому, кем, имена, сроки, цепочка.
func printCert(arg string, asJSON bool) error {
	host, port := netinfo.HostPort(arg, "443")
	c := certJSONOf(host, port)
	if c.Subject == "" && c.Error != "" {
		return ui.Errorf("%s:%s: %s", host, port, c.Error)
	}
	facts := report.Facts{{Key: "кому", Value: c.Subject}, {Key: "кем", Value: strings.TrimSpace(c.Issuer + "  " + c.IssuerOrg)},
		{Key: "имена", Value: strings.Join(c.Names, " ")}, {Key: "с", Value: c.NotBefore.Format("2006-01-02")},
		daysFact("до", c.NotAfter), {Key: "tls", Value: c.TLS}}
	for i, name := range c.Chain {
		facts = append(facts, report.Fact{Key: fmt.Sprintf("цепочка %d", i+1), Value: name, Level: report.Info})
	}
	verify := report.Fact{Key: "проверка", Value: "цепочка проверена", Level: report.OK}
	if !c.Verified {
		verify.Value, verify.Level = "не прошла: "+c.Error, report.Bad
	}
	facts = append(facts, verify)
	return view.Show(report.Report{Schema: "tim.cert/1", Title: host + ":" + port, Data: c, Blocks: []report.Block{facts}}, asJSON)
}

// printExpiries: сроки сертификатов списком; истекающие за две недели и с ошибкой - повод разобраться.
func printExpiries(cfg *config.Config, hosts []string, asJSON bool) error {
	if len(hosts) == 0 {
		hosts = siteHosts(cfg, nil)
	}
	if len(hosts) == 0 {
		return ui.Errorf("доменов нет: tim cert <домен> или список sites в конфиге")
	}
	certs := par.Map(hosts, par.Limit, func(h string) certJSON {
		host, port := netinfo.HostPort(h, "443")
		return certJSONOf(host, port)
	})
	t := report.Table{Columns: []report.Column{{Title: "ХОСТ", Style: report.Key}, {Title: "СРОК"}, {Title: "КЕМ", Style: report.Muted}, {Title: ""}}}
	bad := 0
	for _, c := range certs {
		if c.Subject == "" && c.Error != "" {
			bad++
			t.Rows = append(t.Rows, []any{report.Mark{Text: c.Host, Level: report.Bad}, report.Mark{Text: c.Error, Level: report.Info}, "", ""})
			continue
		}
		days := daysFact("", c.NotAfter)
		note := report.Mark{}
		if !c.Verified {
			note = report.Mark{Text: ui.ShortErr(errors.New(c.Error)), Level: report.Bad}
		}
		if !c.Verified || days.Level >= report.Warn {
			bad++
		}
		t.Rows = append(t.Rows, []any{c.Host, report.Mark{Text: days.Value, Level: days.Level}, c.Issuer, note})
	}
	err := view.Show(report.Report{Schema: "tim.cert.list/1", Data: certs, Blocks: []report.Block{t}}, asJSON)
	if err == nil && bad > 0 {
		return ui.Verdictf("требуют внимания: %d из %d", bad, len(certs))
	}
	return err
}

// printSite: что за сайт - адреса, регистрация, сертификат, ответ HTTP.
func printSite(arg string, asJSON bool) error {
	host, _ := netinfo.HostPort(arg, "")
	target := arg
	if !strings.Contains(target, "://") {
		target = "https://" + host
	}
	r := Collect(host, target)
	var facts report.Facts
	if len(r.IPs) > 0 {
		ip := strings.Join(r.IPs, " ")
		if g := r.Geo; g != nil {
			ip += fmt.Sprintf("  %s, %s, %s AS%d", g.Country, g.City, g.ISP, g.ASN)
		}
		facts = append(facts, report.Fact{Key: "ip", Value: ip})
	}
	if d := r.Domain; d != nil {
		facts = append(facts, report.Fact{Key: "домен", Value: strings.TrimSpace(ui.Dash(d.Registrar) + "  " + d.Source)})
		if !d.Created.IsZero() {
			facts = append(facts, report.Fact{Key: "создан", Value: d.Created.Format("2006-01-02")})
		}
		if !d.Expires.IsZero() {
			facts = append(facts, daysFact("оплачен", d.Expires))
		}
		facts = append(facts, report.Fact{Key: "ns", Value: strings.Join(d.Nameservers, " ")})
	}
	if t := r.TLS; t != nil {
		tls := daysFact("tls", t.Expires)
		tls.Value = t.Issuer + ", до " + tls.Value
		if !t.Verified {
			tls.Value, tls.Level = tls.Value+", не проверен", report.Bad
		}
		facts = append(facts, tls)
	}
	if p := r.HTTP; p != nil {
		facts = append(facts, report.Fact{Key: "http", Value: fmt.Sprintf("%d %s", p.Status, p.Final)})
		for _, rd := range p.Redirects {
			facts = append(facts, report.Fact{Key: "через", Value: rd, Level: report.Info})
		}
		facts = append(facts, report.Fact{Key: "сервер", Value: strings.TrimSpace(p.Server + " " + p.PoweredBy)},
			report.Fact{Key: "cms", Value: ui.Dash(p.CMS)})
	}
	for _, k := range slices.Sorted(maps.Keys(r.Errors)) {
		facts = append(facts, report.Fact{Key: k, Value: r.Errors[k], Level: report.Warn})
	}
	facts = slices.DeleteFunc(facts, func(f report.Fact) bool { return f.Value == "" })
	return view.Show(report.Report{Schema: "tim.site/1", Title: r.Host, Data: r, Blocks: []report.Block{facts}}, asJSON)
}
