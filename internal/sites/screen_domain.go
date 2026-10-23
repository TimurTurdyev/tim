package sites

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/TimurTurdyev/tim/internal/app"
	"github.com/TimurTurdyev/tim/internal/domaudit"
	"github.com/TimurTurdyev/tim/internal/health"
	"github.com/TimurTurdyev/tim/internal/inspect"
	"github.com/TimurTurdyev/tim/internal/level"
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
			{Key: "enter", Label: "подробно", Free: true, Do: func(*tui.BoardRow) tui.Action { return env.Result([]string{"domain", domain, "--fast"}) }},
			{Key: "w", Label: "браузер", Doing: "рисую карту домена", Free: true, Run: func(*tui.BoardRow) (string, error) {
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
		},
	})
}

func domainData(rep *domaudit.Report) tui.BoardData {
	d := tui.BoardData{Status: fmt.Sprintf("имён %d \u00b7 адресов %d", len(rep.Hosts), len(rep.Addrs))}
	d.Lamps = domainLamps(rep.Domain)
	d.Columns = ui.Dim(ui.Pad("ИМЯ", 30) + ui.Pad("АДРЕС", 18) + ui.Pad("ОТВЕТ", 12) + "ЗАМЕТКА")
	v := newDomainView(rep)
	for _, h := range sortHosts(rep) {
		d.Rows = append(d.Rows, v.row(h))
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

// domainLamps: сайт, сертификат, регистрация и почта домена - по лампе на каждое.
func domainLamps(domain string) []string {
	site := health.Site("https://" + domain)
	cert := health.Cert(domain)
	lamps := []string{level.Lamp(site.Level, "сайт "+site.Text), level.Lamp(cert.Level, "сертификат "+cert.Text)}
	if reg, err := inspect.Registration(domain); err == nil && !reg.Expires.IsZero() {
		c := health.Deadline("домен", reg.Expires, health.DomainWarnDays, health.DomainBadDays)
		lamps = append(lamps, level.Lamp(c.Level, "домен "+c.Text)+ui.Dim("  "+reg.Registrar))
	}
	m := inspect.MailCheck(domain, inspect.DKIMSelectors)
	verdict, reasons := m.Verdict()
	switch {
	case m.NullMX:
		lamps = append(lamps, level.Lamp(level.Skip, "почта: домен её не принимает"))
	case verdict == level.Skip:
		lamps = append(lamps, level.Lamp(level.Skip, "почта: "+strings.Join(reasons, ", ")))
	case verdict == level.OK:
		lamps = append(lamps, level.Lamp(level.OK, "почта настроена"))
	case verdict == level.Warn:
		lamps = append(lamps, level.Lamp(level.Warn, "почта: "+strings.Join(reasons, ", ")))
	default:
		lamps = append(lamps, level.Lamp(level.Bad, "почта: "+strings.Join(reasons, ", "))+ui.Dim("  письма могут уходить в спам"))
	}
	return lamps
}

// domainView: отчёт с указателями по адресам для строк имён.
type domainView struct {
	rep   *domaudit.Report
	apex  map[string]bool // адреса апекса: по ним понятно, "тот же сервер" ли у поддомена
	addrs map[string]domaudit.Addr
}

func newDomainView(rep *domaudit.Report) domainView {
	v := domainView{rep: rep, apex: map[string]bool{}, addrs: map[string]domaudit.Addr{}}
	for _, h := range rep.Hosts {
		if h.Name == rep.Domain {
			for _, ip := range h.IPs {
				v.apex[ip] = true
			}
		}
	}
	for _, a := range rep.Addrs {
		v.addrs[a.IP] = a
	}
	return v
}

// row: имя, первый адрес, ответ сайта и заметка; в панели все адреса и что отвечает сервер.
func (v domainView) row(h domaudit.Host) tui.BoardRow {
	row := tui.BoardRow{ID: h.Name, Title: h.Name, Sub: h.Source}
	ip := "-"
	if len(h.IPs) > 0 {
		ip = h.IPs[0]
	}
	status, note := hostStatus(h)
	if note == "" {
		note = v.note(h, ip)
	}
	row.Cells = ui.Fit(h.Name, 29) + " " + ui.Fit(ip, 17) + " " + ui.Pad(status, 12) + ui.Dim(note)
	for _, ip := range h.IPs {
		line := ui.Pad(ip, 17) + " "
		if a, ok := v.addrs[ip]; ok {
			line += ui.Dim(asName(a.AS.Name, a.AS.Country) + " AS" + a.AS.ASN)
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
	switch {
	case h.Takeover != nil:
		row.Pane = append(row.Pane, ui.Bad("можно захватить через "+h.Takeover.Service+": CNAME "+h.Takeover.Target+", "+h.Takeover.Why),
			ui.Dim("\u2192 убрать CNAME в DNS или привязать имя в "+h.Takeover.Service))
	case h.Status == 0 && h.CNAME != "" && len(h.IPs) == 0:
		row.Pane = append(row.Pane, ui.Warn("CNAME на "+h.CNAME+", а такого имени нет: запись устарела"))
	case h.Status == 0:
		row.Pane = append(row.Pane, ui.Warn("имя есть в DNS или логах сертификатов, а сайта нет: запись устарела?"))
	}
	return row
}

// note: чей адрес у имени - тот же сервер, что у апекса, CDN или чужой.
func (v domainView) note(h domaudit.Host, ip string) string {
	a, ok := v.addrs[ip]
	switch {
	case !ok:
		return ""
	case h.Name == v.rep.Domain:
		return asName(a.AS.Name, a.AS.Country)
	case v.apex[ip]:
		return "тот же сервер"
	case a.CDN:
		return "за CDN " + a.AS.Name
	}
	return "другой сервер: " + asName(a.AS.Name, a.AS.Country)
}

// asName: владелец сети без повторов. В RIPE имя часто "AdminVPS - AdminVPS OOO, RU": короткое имя
// повторяет организацию, страна уже в конце.
func asName(name, country string) string {
	name = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(name), ", "+country))
	if short, org, ok := strings.Cut(name, " - "); ok && strings.HasPrefix(org, short) {
		name = org
	}
	return strings.TrimSpace(name + " " + country)
}

// hostStatus: ответ сайта лампой; у молчащих и сломанных сразу и заметка.
func hostStatus(h domaudit.Host) (status, note string) {
	switch {
	case h.Takeover != nil:
		return level.Lamp(level.Bad, "захват"), "CNAME " + h.Takeover.Target + ": " + h.Takeover.Why
	case h.Status == 0 && h.CNAME != "" && len(h.IPs) == 0:
		return level.Lamp(level.Warn, "в никуда"), "CNAME " + h.CNAME + " не резолвится"
	case h.Status == 0:
		return level.Lamp(level.Bad, "молчит"), "не отвечает по HTTP"
	case h.Status >= 500:
		return level.Lamp(level.Bad, strconv.Itoa(h.Status)), "сервер отвечает ошибкой"
	case h.Status >= 400:
		return level.Lamp(level.Warn, strconv.Itoa(h.Status)), ""
	}
	return level.Lamp(level.OK, strconv.Itoa(h.Status)), ""
}

// sortHosts: сам домен первым, за ним имена с проблемами - молчащие и с 5xx, потом с 4xx.
func sortHosts(rep *domaudit.Report) []domaudit.Host {
	hosts := append([]domaudit.Host(nil), rep.Hosts...)
	rank := func(h domaudit.Host) int {
		switch {
		case h.Takeover != nil:
			return 4
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
