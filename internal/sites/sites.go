// Package sites: проверки сайтов и доменов, собранные из inspect и health для команд и экранов.
package sites

import (
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/TimurTurdyev/tim/internal/config"
	"github.com/TimurTurdyev/tim/internal/fill"
	"github.com/TimurTurdyev/tim/internal/health"
	"github.com/TimurTurdyev/tim/internal/inspect"
	"github.com/TimurTurdyev/tim/internal/netinfo"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/sysinfo"
)

// EnvGit: два вопроса к git про .env; ответ "нет" при любой ошибке, чтобы не шуметь.
func EnvGit(dir string) health.EnvGit {
	var e health.EnvGit
	if _, err := os.Stat(filepath.Join(dir, ".env")); err == nil {
		e.Exists = true
		_, err := runner.Output("git", "-C", dir, "check-ignore", "-q", ".env")
		e.Ignored = err == nil
	}
	_, err := runner.Output("git", "-C", dir, "ls-files", "--error-unmatch", ".env")
	e.Tracked = err == nil
	return e
}

// GitCheck: состояние клона. Сам запуск git остаётся здесь, решение об уровне - в health.
func GitCheck(dir string) health.Check {
	if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
		return health.Check{Name: "git", Level: health.Skip, Text: "-"}
	}
	g, ok := sysinfo.GitStatus(dir)
	if !ok {
		return health.Check{Name: "git", Level: health.Skip, Text: "-"}
	}
	return health.GitCheck(g, EnvGit(dir))
}

// CheckRow: все проверки одного проекта. Сетевые идут параллельно; local и net оставляют только
// локальные или только сетевые.
func CheckRow(p *config.Project, dir string, local, net bool) health.Row {
	r := health.Row{Name: p.Name}
	var mu sync.Mutex
	add := func(c health.Check) {
		mu.Lock()
		r.Checks = append(r.Checks, c)
		mu.Unlock()
	}
	var wg sync.WaitGroup
	if !net {
		add(GitCheck(dir))
	}
	host, _ := netinfo.HostPort(p.URL, "")
	if !local && p.URL != "" && host != "" && !fill.Local(host) {
		wg.Go(func() { add(health.Site(p.URL)) })
		wg.Go(func() { add(health.Cert(host)) })
		base := inspect.Registrable(host)
		wg.Go(func() { add(health.Mail(base)) })
		wg.Go(func() { add(health.DomainReg(base)) })
	}
	wg.Wait()
	return r
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
	var mu sync.Mutex
	fail := func(k string, err error) {
		mu.Lock()
		r.Errors[k] = err.Error()
		mu.Unlock()
	}
	var wg sync.WaitGroup
	wg.Go(func() {
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
	})
	wg.Go(func() {
		if d, err := inspect.Registration(inspect.Registrable(host)); err == nil {
			r.Domain = d
		} else {
			fail("domain", err)
		}
	})
	wg.Go(func() {
		info, err := inspect.Cert(host, "443")
		if err != nil {
			fail("tls", err)
			return
		}
		r.TLS = &TLS{Issuer: info.Leaf().Issuer.CommonName, Expires: info.Leaf().NotAfter, Verified: info.VerifyErr == nil}
	})
	wg.Go(func() {
		if p, err := inspect.HTTP(target); err == nil {
			r.HTTP = p
		} else {
			fail("http", err)
		}
	})
	wg.Wait()
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
	rows := make([]Expiry, len(hosts))
	var wg sync.WaitGroup
	for i, h := range hosts {
		wg.Go(func() {
			host, port := netinfo.HostPort(h, "443")
			rows[i].Host = host
			info, err := inspect.Cert(host, port)
			if err != nil {
				rows[i].Err = err
				return
			}
			rows[i].Expires, rows[i].Issuer = info.Leaf().NotAfter, info.Leaf().Issuer.CommonName
			if info.VerifyErr != nil {
				rows[i].Err = info.VerifyErr
			}
		})
	}
	wg.Wait()
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
	Domain, Level string
	Missing       []string
}

// MailVerdicts: почта по доменам, параллельно, в том же порядке.
func MailVerdicts(domains []string) []MailVerdict {
	rows := make([]MailVerdict, len(domains))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for i, d := range domains {
		wg.Go(func() {
			sem <- struct{}{}
			defer func() { <-sem }()
			m := inspect.MailCheck(d, inspect.DKIMSelectors)
			level, missing := m.Verdict()
			rows[i] = MailVerdict{d, level, missing}
		})
	}
	wg.Wait()
	return rows
}

// dkimRevoked: тег p= пустой - ключ отозван.
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
