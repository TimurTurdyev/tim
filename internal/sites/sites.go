// Package sites: проверки сайтов и доменов, собранные из inspect и health для команд и экранов.
package sites

import (
	"net"
	"net/netip"
	"sort"
	"strings"
	"time"

	"github.com/TimurTurdyev/tim/internal/health"
	"github.com/TimurTurdyev/tim/internal/inspect"
	"github.com/TimurTurdyev/tim/internal/level"
	"github.com/TimurTurdyev/tim/internal/netinfo"
	"github.com/TimurTurdyev/tim/internal/par"
	"github.com/TimurTurdyev/tim/internal/servers"
	"github.com/TimurTurdyev/tim/report"
)

// DomainChecks: почта и регистрация домена на один обход - у поддоменов домен общий, и спрашивать его
// DNS и whois по разу на каждый незачем.
type DomainChecks struct {
	mail, reg par.Memo[string, health.Check]
}

// SiteRow: проверки сайта - ответ, сертификат, почта и срок регистрации его домена; параллельно.
func SiteRow(host string, shared *DomainChecks) health.Row {
	base := inspect.Registrable(host)
	checks := []func() health.Check{
		func() health.Check { return health.Site("https://" + host) },
		func() health.Check { return health.Cert(host) },
		func() health.Check { return shared.mail.Get(base, func() health.Check { return health.Mail(base) }) },
		func() health.Check {
			return shared.reg.Get(base, func() health.Check { return health.DomainReg(base) })
		},
	}
	run := func(check func() health.Check) health.Check { return check() }
	return health.Row{Name: host, Checks: par.Map(checks, len(checks), run)}
}

// ServerRow: сервер строкой обхода - тем же сбором, что экран сервера.
func ServerRow(alias string) health.Row {
	return health.Row{Name: alias, Checks: []health.Check{servers.CheckServer(alias)}}
}

// localHost: имя, которого нет в интернете, - проверять его снаружи бессмысленно.
func localHost(host string) bool {
	if a, err := netip.ParseAddr(host); err == nil {
		return a.IsPrivate() || a.IsLoopback()
	}
	for _, z := range []string{"localhost", ".localhost", ".test", ".local", ".lan", ".internal", ".example"} {
		if host == strings.TrimPrefix(z, ".") || strings.HasSuffix(host, z) {
			return true
		}
	}
	return !strings.Contains(host, ".")
}

// Report: что за сайт - адреса, регистрация, сертификат, ответ HTTP.
type Report struct {
	Host   string            `json:"host"`
	IPs    []string          `json:"ips,omitempty"`
	Geo    *inspect.Geo      `json:"geo,omitempty"`
	Domain *inspect.Domain   `json:"domain,omitempty"`
	TLS    *TLS              `json:"tls,omitempty"`
	HTTP   *inspect.Page     `json:"http,omitempty"`
	Errors map[string]string `json:"errors,omitempty"`
}

type TLS struct {
	Issuer   string    `json:"issuer"`
	Expires  time.Time `json:"expires"`
	Verified bool      `json:"verified"`
}

// Collect: всё о сайте разом и параллельно.
func Collect(host, target string) *Report {
	r := &Report{Host: host, Errors: map[string]string{}}
	var dnsErr, geoErr, domainErr, tlsErr, httpErr error
	par.All(
		func() {
			if r.IPs, dnsErr = net.LookupHost(host); dnsErr == nil {
				r.Geo, geoErr = inspect.IPGeo(r.IPs[0])
			}
		},
		func() { r.Domain, domainErr = inspect.Registration(inspect.Registrable(host)) },
		func() {
			info, err := inspect.Cert(host, "443")
			if tlsErr = err; err == nil {
				r.TLS = &TLS{Issuer: info.Leaf().Issuer.CommonName, Expires: info.Leaf().NotAfter, Verified: info.VerifyErr == nil}
			}
		},
		func() { r.HTTP, httpErr = inspect.HTTP(target) },
	)
	for k, err := range map[string]error{"dns": dnsErr, "geo": geoErr, "domain": domainErr, "tls": tlsErr, "http": httpErr} {
		if err != nil {
			r.Errors[k] = err.Error()
		}
	}
	return r
}

// Expiry: срок сертификата одного хоста; Err - не получен или не прошёл проверку.
type Expiry struct {
	Host    string
	Expires time.Time
	Issuer  string
	Err     error
}

// Expiries: сертификаты списком, параллельно; проблемные и ближайшие по сроку первыми.
func Expiries(hosts []string) []Expiry {
	rows := par.Map(hosts, par.Limit, func(h string) Expiry {
		host, port := netinfo.HostPort(h, "443")
		info, err := inspect.Cert(host, port)
		if err != nil {
			return Expiry{Host: host, Err: err}
		}
		return Expiry{Host: host, Expires: info.Leaf().NotAfter, Issuer: info.Leaf().Issuer.CommonName, Err: info.VerifyErr}
	})
	sort.SliceStable(rows, func(a, b int) bool {
		if (rows[a].Err != nil) != (rows[b].Err != nil) {
			return rows[a].Err != nil
		}
		return rows[a].Expires.Before(rows[b].Expires)
	})
	return rows
}

// MailVerdict: итог проверки почты домена.
type MailVerdict struct {
	Domain  string
	Level   level.Level
	Missing []string
}

// Finding: итог почты домена как находка отчёта.
func (v MailVerdict) Finding() report.Finding {
	return report.Finding{Area: "почта", Subject: v.Domain, Level: v.Level, Text: strings.Join(v.Missing, ", ")}
}

// MailVerdicts: почта по доменам, параллельно, в том же порядке.
func MailVerdicts(domains []string) []MailVerdict {
	return par.Map(domains, par.Limit, func(d string) MailVerdict {
		verdict, missing := inspect.MailCheck(d, inspect.DKIMSelectors).Verdict()
		return MailVerdict{d, verdict, missing}
	})
}

// DKIMRevoked: запись есть, но ключ пустой - селектор отозван.
func DKIMRevoked(rec string) bool {
	for _, part := range strings.Split(rec, ";") {
		k, v, ok := strings.Cut(strings.TrimSpace(part), "=")
		if ok && strings.TrimSpace(k) == "p" {
			return strings.TrimSpace(v) == ""
		}
	}
	return false
}
