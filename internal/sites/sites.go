// Package sites: проверки сайтов и доменов, собранные из inspect и health для команд и экранов.
package sites

import (
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/TimurTurdyev/tim/internal/config"
	"github.com/TimurTurdyev/tim/internal/fill"
	"github.com/TimurTurdyev/tim/internal/gitx"
	"github.com/TimurTurdyev/tim/internal/health"
	"github.com/TimurTurdyev/tim/internal/inspect"
	"github.com/TimurTurdyev/tim/internal/level"
	"github.com/TimurTurdyev/tim/internal/netinfo"
	"github.com/TimurTurdyev/tim/internal/par"
	"github.com/TimurTurdyev/tim/internal/srvinfo"
	"github.com/TimurTurdyev/tim/internal/sshx"
)

// EnvGit: два вопроса к git про .env; ответ "нет" при любой ошибке, чтобы не шуметь.
func EnvGit(dir string) health.EnvGit {
	env := filepath.Join(dir, ".env")
	e := health.EnvGit{Tracked: gitx.Tracked(env)}
	if _, err := os.Stat(env); err == nil {
		e.Exists, e.Ignored = true, gitx.Ignored(env)
	}
	return e
}

// GitCheck: состояние клона. Сам запуск git остаётся здесь, решение об уровне - в health.
func GitCheck(dir string) health.Check {
	if !gitx.IsRepo(dir) {
		return health.Check{Name: "git", Level: level.Skip, Text: "-"}
	}
	g, ok := gitx.Status(dir)
	if !ok {
		return health.Check{Name: "git", Level: level.Skip, Text: "-"}
	}
	return health.GitCheck(g, EnvGit(dir))
}

// DomainChecks: почта и регистрация домена на один обход - у проектов на поддоменах домен общий,
// и спрашивать его DNS и whois по разу на проект незачем.
type DomainChecks struct {
	mail, reg, server par.Memo[string, health.Check]
}

// CheckRow: все проверки одного проекта. Сетевые идут параллельно; local и net оставляют только
// локальные или только сетевые. servers - ещё и сервер проекта по ssh: один заход на хост, сколько бы
// проектов на нём ни жило.
func CheckRow(p *config.Project, dir string, local, net, servers bool, shared *DomainChecks) health.Row {
	var checks []func() health.Check
	if !net {
		checks = append(checks, func() health.Check { return GitCheck(dir) })
	}
	host, _ := netinfo.HostPort(p.URL, "")
	if !local && p.URL != "" && host != "" && !fill.Local(host) {
		base := inspect.Registrable(host)
		mail := func() health.Check { return health.Mail(base) }
		reg := func() health.Check { return health.DomainReg(base) }
		checks = append(checks,
			func() health.Check { return health.Site(p.URL) },
			func() health.Check { return health.Cert(host) },
			func() health.Check { return shared.mail.Get(base, mail) },
			func() health.Check { return shared.reg.Get(base, reg) },
		)
	}
	if servers && !local && p.SSH != "" {
		checks = append(checks, func() health.Check {
			return shared.server.Get(serverKey(p.SSH), func() health.Check { return srvinfo.CheckServer(p.SSH) })
		})
	}
	run := func(check func() health.Check) health.Check { return check() }
	return health.Row{Name: p.Name, Checks: par.Map(checks, len(checks), run)}
}

// serverKey: один хост под разными алиасами проверяется один раз.
func serverKey(alias string) string {
	if t := sshx.Resolve(alias); t.Host != "" {
		return t.Host + ":" + t.Port
	}
	return alias
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
