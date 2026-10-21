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
	if app.Interactive() && !o.json && !o.openMap && o.mapOut == "" && !o.fast {
		return env.Open(DomainScreen(env, name))
	}
	notify := func(s string) { ui.Println(ui.Dim(s)) }
	if o.json {
		notify = nil
	}
	rep := domaudit.Collect(name, domaudit.Options{SkipCRT: o.fast, FreshCRT: o.fresh, SkipNeighbors: o.noNeighbors, Notify: notify})
	if o.json {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(rep)
	}
	printDomain(rep, o.openMap || o.mapOut != "")
	if o.openMap || o.mapOut != "" {
		return writeMap(rep, o.mapOut, o.openMap)
	}
	return nil
}

// DomainOf: домен из аргумента, поля url проекта или проекта текущей папки.
func DomainOf(env *app.Env, arg string) (string, error) {
	if arg != "" {
		if p, ok := env.Cfg.Project(arg); ok && p.URL != "" {
			h, _ := netinfo.HostPort(p.URL, "")
			return inspect.Registrable(h), nil
		}
		h, _ := netinfo.HostPort(arg, "")
		if !strings.Contains(h, ".") {
			return "", ui.Errorf("не домен и не проект с полем url: %s", arg)
		}
		return h, nil
	}
	p, err := env.Project("")
	if err != nil {
		return "", ui.Errorf("укажи домен: tim domain example.com")
	}
	if p.URL == "" {
		return "", ui.Errorf("у проекта %s нет поля url: tim domain <домен>", p.Name)
	}
	h, _ := netinfo.HostPort(p.URL, "")
	return h, nil
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

func printDomain(rep *domaudit.Report, withMap bool) {
	ui.Println(ui.Info(rep.Domain))
	if d, err := inspect.Registration(rep.Domain); err == nil {
		line := ui.Label("регистратор ") + ui.Dash(d.Registrar)
		if !d.Expires.IsZero() {
			line += "  до " + ui.DaysLeft(d.Expires)
		}
		ui.Println(line)
		if len(d.Nameservers) > 0 {
			ui.Println(ui.Label("ns          ") + strings.Join(d.Nameservers, " "))
		}
	}
	printMailLine(rep.Domain)
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
func printMailLine(domain string) {
	m := inspect.MailCheck(domain, inspect.DKIMSelectors)
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
		if h.Status == 0 {
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
		if h.Status == 0 {
			return ui.Pad(name, 34) + " " + ui.Dim("не отвечает по HTTP  "+h.Source)
		}
		code := strconv.Itoa(h.Status)
		switch {
		case h.Status >= 500:
			code = ui.Bad(code)
		case h.Status >= 400:
			code = ui.Warn(code)
		default:
			code = ui.Label(code)
		}
		var rest []string
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

// printDomains: кратко по всем доменам реестра.
func printDomains(cfg *config.Config) error {
	domains := Domains(cfg)
	if len(domains) == 0 {
		return ui.Errorf("ни у одного проекта нет поля url: tim proj add --url")
	}
	sort.Strings(domains)
	for _, d := range domains {
		rep := domaudit.Collect(d, domaudit.Options{SkipCRT: true, SkipNeighbors: true})
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
