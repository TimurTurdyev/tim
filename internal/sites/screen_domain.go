package sites

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/TimurTurdyev/tim/internal/app"
	"github.com/TimurTurdyev/tim/internal/domaudit"
	"github.com/TimurTurdyev/tim/internal/health"
	"github.com/TimurTurdyev/tim/internal/inspect"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/tui"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// DomainScreen: домен целиком - лампы сайта, сертификата, регистрации и почты, имена с адресами.
func DomainScreen(env *app.Env, domain string) tui.Screen {
	var (
		mu  sync.Mutex
		rep *domaudit.Report
	)
	return tui.NewBoard(tui.BoardOpts{
		Crumbs:  []string{"Сайты", domain},
		Loading: "собираю по " + domain + ": записи, логи сертификатов, адреса, живость имён - до минуты",
		Command: func(*tui.BoardRow) string { return "tim domain " + domain },
		Load: func(fresh bool) tui.BoardData {
			mu.Lock()
			defer mu.Unlock()
			if rep == nil || fresh {
				rep = domaudit.Collect(domain, domaudit.Options{FreshCRT: fresh})
			}
			return domainData(rep)
		},
		Actions: []tui.BoardAction{
			{Key: "enter", Label: "подробно", Do: func(*tui.BoardRow) tui.Action { return env.Result([]string{"domain", domain, "--fast"}) }},
			{Key: "m", Label: "карта в браузере", Run: func(*tui.BoardRow) (string, error) {
				mu.Lock()
				defer mu.Unlock()
				if rep == nil {
					return "", ui.Errorf("отчёта ещё нет")
				}
				path := filepath.Join(os.TempDir(), "tim-domain-"+rep.Domain+".html")
				if err := domaudit.WriteMap(rep, path); err != nil {
					return "", err
				}
				return "карта открыта в браузере", runner.OpenBrowser(path)
			}},
			{Key: "p", Label: "почта", Do: func(*tui.BoardRow) tui.Action { return env.Result([]string{"mail", domain}) }},
		},
	})
}

func domainData(rep *domaudit.Report) tui.BoardData {
	var d tui.BoardData
	d.Status = fmt.Sprintf("имён %d \u00b7 адресов %d", len(rep.Hosts), len(rep.Addrs))
	var lamps []string
	site := health.Site("https://" + rep.Domain)
	lamps = append(lamps, health.Lamp(site.Level, "сайт "+site.Text))
	cert := health.Cert(rep.Domain)
	lamps = append(lamps, health.Lamp(cert.Level, "сертификат "+cert.Text))
	if reg, err := inspect.Registration(rep.Domain); err == nil && !reg.Expires.IsZero() {
		c := health.Deadline("домен", reg.Expires, health.DomainWarnDays, health.DomainBadDays)
		lamps = append(lamps, health.Lamp(c.Level, "домен "+c.Text)+ui.Dim("  "+reg.Registrar))
	}
	m := inspect.MailCheck(rep.Domain, inspect.DKIMSelectors)
	level, reasons := m.Verdict()
	switch {
	case m.NullMX:
		lamps = append(lamps, ui.Dim("- почта: домен её не принимает"))
	case level == "ok":
		lamps = append(lamps, ui.OK("\u25cf почта настроена"))
	case level == "warn":
		lamps = append(lamps, ui.Warn("\u25b2 почта: "+strings.Join(reasons, ", ")))
	default:
		lamps = append(lamps, ui.Bad("\u2715 почта: "+strings.Join(reasons, ", "))+ui.Dim("  письма могут уходить в спам"))
	}
	for i := 0; i+1 < len(lamps); i += 2 {
		d.Head = append(d.Head, ui.Pad(lamps[i], 52)+lamps[i+1])
	}
	if len(lamps)%2 == 1 {
		d.Head = append(d.Head, lamps[len(lamps)-1])
	}
	// Адреса апекса: по ним понятно, "тот же сервер" ли у поддомена.
	apex := map[string]bool{}
	for _, h := range rep.Hosts {
		if h.Name == rep.Domain {
			for _, ip := range h.IPs {
				apex[ip] = true
			}
		}
	}
	as := map[string]domaudit.Addr{}
	for _, a := range rep.Addrs {
		as[a.IP] = a
	}
	d.Head = append(d.Head, "", ui.Dim(ui.Pad("ИМЯ", 30)+ui.Pad("АДРЕС", 18)+ui.Pad("ОТВЕТ", 12)+"ЗАМЕТКА"))
	hosts := sortHosts(rep)
	for _, h := range hosts {
		row := tui.BoardRow{ID: h.Name, Title: h.Name, Sub: h.Source}
		ip := "-"
		if len(h.IPs) > 0 {
			ip = h.IPs[0]
		}
		var status, note string
		switch {
		case h.Status == 0:
			status = ui.Bad("\u2715 молчит")
			note = "не отвечает по HTTP"
		case h.Status >= 500:
			status = ui.Bad(fmt.Sprintf("\u2715 %d", h.Status))
			note = "сервер отвечает ошибкой"
		case h.Status >= 400:
			status = ui.Warn(fmt.Sprintf("\u25b2 %d", h.Status))
		default:
			status = ui.OK(fmt.Sprintf("\u25cf %d", h.Status))
		}
		if a, ok := as[ip]; ok && note == "" {
			switch {
			case h.Name == rep.Domain:
				note = strings.TrimSpace(a.AS.Name + " " + a.AS.Country)
			case apex[ip]:
				note = "тот же сервер"
			case a.CDN:
				note = "за CDN " + a.AS.Name
			default:
				note = "другой сервер: " + strings.TrimSpace(a.AS.Name+" "+a.AS.Country)
			}
		}
		row.Cells = ui.Pad(h.Name, 30) + ui.Pad(ip, 18) + ui.Pad(status, 12) + ui.Dim(note)
		for _, ip := range h.IPs {
			line := ui.Pad(ip, 18)
			if a, ok := as[ip]; ok {
				line += ui.Dim(strings.TrimSpace(a.AS.Name + " " + a.AS.Country + " AS" + a.AS.ASN))
				if a.PTR != "" {
					line += ui.Dim("  " + a.PTR)
				}
			}
			row.Pane = append(row.Pane, line)
		}
		var about []string
		for _, t := range []string{h.Server, h.Title} {
			if t != "" {
				about = append(about, t)
			}
		}
		if h.Final != "" {
			about = append(about, "-> "+h.Final)
		}
		if len(about) > 0 {
			row.Pane = append(row.Pane, ui.Dim(strings.Join(about, "  ")))
		}
		if h.Status == 0 {
			row.Pane = append(row.Pane, ui.Warn("имя есть в DNS или логах сертификатов, а сайта нет: запись устарела?"))
		}
		d.Rows = append(d.Rows, row)
	}
	if len(rep.Related) > 0 {
		d.Foot = append(d.Foot, ui.Dim("в тех же сертификатах: "+strings.Join(cut(rep.Related, 8), " ")))
	}
	for _, e := range rep.Errors {
		d.Foot = append(d.Foot, ui.Warn("! "+e))
	}
	d.Empty = "ни одно имя не резолвится: домена нет, он не делегирован или DNS недоступен"
	return d
}

// sortHosts: сам домен первым, за ним имена с проблемами - молчащие и с 5xx, потом с 4xx.
func sortHosts(rep *domaudit.Report) []domaudit.Host {
	hosts := append([]domaudit.Host(nil), rep.Hosts...)
	rank := func(h domaudit.Host) int {
		switch {
		case h.Name == rep.Domain:
			return 3
		case h.Status == 0 || h.Status >= 500:
			return 2
		case h.Status >= 400:
			return 1
		}
		return 0
	}
	sort.SliceStable(hosts, func(i, j int) bool { return rank(hosts[i]) > rank(hosts[j]) })
	return hosts
}
