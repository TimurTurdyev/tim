package sites

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/TimurTurdyev/tim/internal/app"
	"github.com/TimurTurdyev/tim/internal/config"
	"github.com/TimurTurdyev/tim/internal/domaudit"
	"github.com/TimurTurdyev/tim/internal/inspect"
	"github.com/TimurTurdyev/tim/internal/level"
	"github.com/TimurTurdyev/tim/internal/netinfo"
	"github.com/TimurTurdyev/tim/internal/par"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// domain: отчёт по домену - экраном в терминале, иначе строками, JSON или картой в браузере.
func domain(env *app.Env, arg string, o domainOptions) error {
	if o.all {
		return printDomains(env.Cfg)
	}
	name, err := DomainOf(env, arg)
	if err != nil {
		return err
	}
	if o.twins {
		return printTwins(name, o.json)
	}
	if app.Interactive() && !o.json && !o.openMap && o.mapOut == "" && !o.fast {
		return env.Open(DomainScreen(env, name))
	}
	notify := func(s string) { ui.Println(ui.Dim(s)) }
	if o.json {
		notify = nil
	}
	opt := domaudit.Options{Quiet: o.fast, FreshCRT: o.fresh, SkipNeighbors: o.noNeighbors, Notify: notify}
	if o.json {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(domaudit.Collect(name, opt))
	}
	var rep *domaudit.Report
	var about domainAbout
	par.All(func() { rep = domaudit.Collect(name, opt) }, func() { about = aboutDomain(name) })
	printDomain(rep, about, o.openMap || o.mapOut != "")
	if o.openMap || o.mapOut != "" {
		return writeMap(rep, o.mapOut, o.openMap)
	}
	return nil
}

// DomainOf: домен из аргумента - имя, адрес сайта или url; имя на национальном языке - в том виде, в каком оно в DNS.
func DomainOf(env *app.Env, arg string) (string, error) {
	if arg == "" {
		return "", ui.Errorf("укажи домен: tim domain example.com")
	}
	h, _ := netinfo.HostPort(arg, "")
	t, err := netinfo.ParseTarget(h)
	if err != nil || t.Kind != netinfo.KindDomain {
		return "", ui.Errorf("%s - не домен: tim domain example.com", arg)
	}
	return t.Value, nil
}

func writeMap(rep *domaudit.Report, path string, open bool) error {
	if path == "" {
		path = filepath.Join(os.TempDir(), "tim-domain-"+rep.Domain+".html")
	}
	if err := domaudit.WriteMap(rep, path); err != nil {
		return err
	}
	ui.Println(ui.Label("карта ") + path)
	if !open {
		return nil
	}
	return runner.OpenBrowser(path)
}

// domainAbout: регистрация и почта домена; nil - не узнали.
type domainAbout struct {
	reg  *inspect.Domain
	mail *inspect.Mail
}

func aboutDomain(domain string) domainAbout {
	var a domainAbout
	par.All(func() {
		if d, err := inspect.Registration(domain); err == nil {
			a.reg = d
		}
	}, func() { a.mail = inspect.MailCheck(domain, inspect.DKIMSelectors) })
	return a
}

func printDomain(rep *domaudit.Report, about domainAbout, withMap bool) {
	ui.Println(ui.Info(rep.Domain))
	if d := about.reg; d != nil {
		line := ui.Label("регистратор ") + ui.Dash(d.Registrar)
		if !d.Expires.IsZero() {
			line += "  до " + ui.DaysLeft(d.Expires)
		}
		ui.Println(line)
		if len(d.Nameservers) > 0 {
			ui.Println(ui.Label("ns          ") + strings.Join(d.Nameservers, " "))
		}
	}
	printMailLine(rep.Domain, about.mail)
	if len(rep.Hosts) == 0 {
		ui.Println(ui.Bad("ни одно имя не резолвится: домена нет, он не делегирован или DNS недоступен"))
		for _, e := range rep.Errors {
			ui.Println(ui.Warn("! ") + ui.Dim(e))
		}
		return
	}
	printAddrs(rep)
	printDomainNotes(rep)
	if !withMap {
		ui.Println(ui.Dim("карта в браузере: tim domain " + rep.Domain + " --map"))
	}
}

// printMailLine: почта домена одной строкой - подробно её показывает tim mail.
func printMailLine(domain string, m *inspect.Mail) {
	switch {
	case m.NullMX:
		ui.Println(ui.Label("mx          ") + ui.Dim("null MX: домен почту не принимает"))
	case len(m.MX) == 0:
		ui.Println(ui.Label("mx          ") + ui.Dim("нет"))
	default:
		ui.Println(ui.Label("mx          ") + strings.Join(m.MX, ", "))
	}
	verdict, reasons := m.Verdict()
	switch verdict {
	case level.Skip:
		ui.Println(ui.Label("почта       ") + ui.Dim("не проверено: "+strings.Join(reasons, ", ")))
	case level.OK:
		ui.Println(ui.Label("почта       ") + "настроена верно")
	case level.Warn:
		ui.Println(ui.Label("почта       ") + ui.Warn(strings.Join(reasons, ", ")) + ui.Dim("  подробно: tim mail "+domain))
	default:
		ui.Println(ui.Label("почта       ") + ui.Bad("проблемы: "+strings.Join(reasons, ", ")) + ui.Dim("  подробно: tim mail "+domain))
	}
}

func printAddrs(rep *domaudit.Report) {
	ui.Println()
	ui.Println(ui.Info("Адреса и имена"))
	for _, a := range rep.Addrs {
		var parts []string
		if a.Internal {
			parts = append(parts, "внутренний адрес")
		}
		if owner := asName(a.AS.Name, a.AS.Country); owner != "" {
			parts = append(parts, owner)
		}
		if a.AS.ASN != "" {
			parts = append(parts, "AS"+a.AS.ASN)
		}
		if a.PTR != "" {
			parts = append(parts, a.PTR)
		}
		ui.Println(ui.Label(ui.Pad(a.IP, 22)) + " " + ui.Dim(strings.Join(parts, "  ")))
		for _, name := range a.Hosts {
			ui.Println("   " + hostLine(rep, name))
		}
		if len(a.Shared) > 0 {
			ui.Println("   " + ui.Dim("на этом адресе ещё: "+strings.Join(cut(a.Shared, 8), " ")))
		}
	}
}

func printDomainNotes(rep *domaudit.Report) {
	dead := 0
	for _, h := range rep.Hosts {
		if h.Silent() {
			dead++
		}
	}
	ui.Println()
	ui.Println(ui.Dim("имён " + strconv.Itoa(len(rep.Hosts)) + ", адресов " + strconv.Itoa(len(rep.Addrs)) +
		", не отвечают по HTTP " + strconv.Itoa(dead)))
	if !rep.CRTCached.IsZero() {
		ui.Println(ui.Dim("список имён из кэша от " + rep.CRTCached.Format("02.01 15:04") + ", обновить: --fresh"))
	}
	for _, h := range rep.Hosts {
		if h.Takeover != nil {
			ui.Println(ui.Bad("! "+h.Name+" можно захватить через "+h.Takeover.Service+": "+h.Takeover.Why) +
				ui.Dim("  убрать CNAME в DNS или привязать имя в "+h.Takeover.Service))
		}
	}
	if len(rep.Related) > 0 {
		ui.Println(ui.Label("в тех же сертификатах: ") + strings.Join(cut(rep.Related, 10), " "))
	}
	if len(rep.Trackers) > 0 {
		var t []string
		for _, x := range rep.Trackers {
			t = append(t, x.Kind+" "+x.ID)
		}
		ui.Println(ui.Label("счётчики:    ") + strings.Join(t, ", "))
		ui.Println(ui.Dim("             поиск сайтов с тем же счётчиком: https://publicwww.com/websites/" + rep.Trackers[0].ID + "/"))
	}
	for _, n := range rep.Notes {
		ui.Println(ui.Dim(n))
	}
	for _, e := range rep.Errors {
		ui.Println(ui.Warn("! ") + ui.Dim(e))
	}
}

func hostLine(rep *domaudit.Report, name string) string {
	for _, h := range rep.Hosts {
		if h.Name != name {
			continue
		}
		if h.Takeover != nil {
			return ui.Pad(name, 34) + " " + ui.Bad("захват! ") + ui.Dim("CNAME "+h.Takeover.Target+", "+h.Takeover.Why)
		}
		if h.Status == 0 && len(h.IPs) == 0 {
			return ui.Pad(name, 34) + " " + ui.Warn("CNAME в никуда ") + ui.Dim(h.CNAME+" не резолвится  "+h.Source)
		}
		if h.Internal {
			return ui.Pad(name, 34) + " " + ui.Warn("внутренний адрес ") + ui.Dim("по HTTP не спрашивали: в интернете за ним никого  "+h.Source)
		}
		if h.Unchecked {
			return ui.Pad(name, 34) + " " + ui.Dim("по HTTP не спрашивали  "+h.Source)
		}
		if h.Status == 0 {
			return ui.Pad(name, 34) + " " + ui.Dim("не отвечает по HTTP  "+h.Source)
		}
		code := strconv.Itoa(h.Status)
		switch {
		case h.Status >= 500:
			code = ui.Bad(code)
		case h.Status >= 400 || h.CertError != "":
			code = ui.Warn(code)
		default:
			code = ui.Label(code)
		}
		var rest []string
		if h.CertError != "" {
			rest = append(rest, "сертификат: "+h.CertError)
		}
		for _, t := range []string{h.Server, h.Title} {
			if t != "" {
				rest = append(rest, t)
			}
		}
		if h.Final != "" {
			rest = append(rest, "-> "+h.Final)
		}
		return ui.Pad(name, 34) + " " + code + " " + ui.Dim(strings.Join(rest, "  "))
	}
	return name
}

// cut: первые n и "и ещё N".
func cut(list []string, n int) []string {
	if len(list) <= n {
		return list
	}
	return append(list[:n:n], "и ещё "+strconv.Itoa(len(list)-n))
}

// printDomains: кратко по всем доменам сайтов.
func printDomains(cfg *config.Config) error {
	domains := roundDomains(siteHosts(cfg, nil))
	if len(domains) == 0 {
		return ui.Errorf("сайтов нет: добавь их в конфиг - sites = [\"example.com\"]")
	}
	sort.Strings(domains)
	for _, d := range domains {
		rep := domaudit.Collect(d, domaudit.Options{Quiet: true})
		live := 0
		for _, h := range rep.Hosts {
			if h.Status > 0 {
				live++
			}
		}
		ui.Println(ui.Label(ui.Pad(d, 30)) + " имён " + ui.Pad(strconv.Itoa(len(rep.Hosts)), 4) +
			" живых " + ui.Pad(strconv.Itoa(live), 4) + " адресов " + strconv.Itoa(len(rep.Addrs)))
	}
	return nil
}
