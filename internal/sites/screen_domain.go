package sites

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/TimurTurdyev/tim/internal/app"
	"github.com/TimurTurdyev/tim/internal/domaudit"
	"github.com/TimurTurdyev/tim/internal/health"
	"github.com/TimurTurdyev/tim/internal/inspect"
	"github.com/TimurTurdyev/tim/internal/level"
	"github.com/TimurTurdyev/tim/internal/par"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/tui"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// domainLoad: один сбор на все вкладки домена; этапы - для заметки, пока он идёт.
type domainLoad struct {
	domain  string
	mu      sync.Mutex // сам сбор: вкладки ждут один и тот же
	rep     *domaudit.Report
	lamps   []string
	smu     sync.Mutex // этапы - их читает заметка, пока сбор держит mu
	stages  []string
	started time.Time
}

// get: отчёт, а по fresh - собрать заново; вкладки ждут один и тот же сбор.
func (l *domainLoad) get(fresh bool) (*domaudit.Report, []string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.rep != nil && !fresh {
		return l.rep, l.lamps
	}
	l.smu.Lock()
	l.started, l.stages = time.Now(), nil
	l.smu.Unlock()
	note := func(s string) {
		l.smu.Lock()
		defer l.smu.Unlock()
		l.stages = append(l.stages, s)
	}
	note("сайт, сертификат, домен и почта")
	par.All(func() { l.lamps = domainLamps(l.domain) },
		func() { l.rep = domaudit.Collect(l.domain, domaudit.Options{FreshCRT: fresh, Notify: note}) })
	return l.rep, l.lamps
}

// wait: заметка сбора - бегунок, секунды и последний этап.
func (l *domainLoad) wait(width int) []string {
	l.smu.Lock()
	stage, started := "лампы сайта, сертификата, почты", l.started
	if n := len(l.stages); n > 0 {
		stage = l.stages[n-1]
	}
	l.smu.Unlock()
	return tui.WaitNotice("Собираю всё по "+l.domain, -1, "обычно до минуты", started, stage+"; если уйти, сбор дойдёт до конца в фоне", width)
}

// DomainScreen: домен целиком вкладками - имена, адреса, записи, сертификат, почта, двойники; лампы сайта,
// сертификата, регистрации и почты над таблицей.
func DomainScreen(env *app.Env, domain string) tui.Screen {
	load := &domainLoad{domain: domain, started: time.Now()}
	mapAction := tui.BoardAction{Key: "w", Label: "браузер", Doing: "рисую карту домена", Free: true, Run: func(*tui.BoardRow) (string, error) {
		rep, _ := load.get(false)
		path := filepath.Join(os.TempDir(), "tim-domain-"+rep.Domain+".html")
		if err := domaudit.WriteMap(rep, path); err != nil {
			return "", err
		}
		return domain + ": карта открыта в браузере", runner.OpenBrowser(path)
	}}
	command := func(*tui.BoardRow) string { return "tim domain " + domain }
	names := tui.NewBoard(tui.BoardOpts{
		Crumbs: []string{"Сайты", domain}, Tab: "Имена", Search: true, Order: "проблемные сверху",
		Command: command, Wait: load.wait,
		Load: func(fresh bool) tui.BoardData { rep, lamps := load.get(fresh); return namesData(rep, lamps) },
		Actions: []tui.BoardAction{
			{Key: "enter", Label: "подробно", Do: func(row *tui.BoardRow) tui.Action { return env.Result([]string{"site", row.ID}) }},
			mapAction,
		},
	}).(tui.Tab)
	addrs := addrsTab(domain, load, command, mapAction)
	mail := mailTab(env, domain)
	return tui.NewTabs([]string{"Сайты", domain}, []tui.Tab{names, addrs, recordsTab(domain), certTab(domain), mail, twinsTab(domain)}, 0)
}

func namesData(rep *domaudit.Report, lamps []string) tui.BoardData {
	d := tui.BoardData{Status: ui.Count(len(rep.Hosts), "имя", "имени", "имён") + " \u00b7 " + ui.Count(len(rep.Addrs), "адрес", "адреса", "адресов"),
		Tab: "Имена " + strconv.Itoa(len(rep.Hosts))}
	d.Head = []string{strings.Join(lamps, "   ")}
	d.Columns = ui.Pad("ИМЯ", 30) + ui.Pad("ОТВЕТ", 12) + "ЗАМЕТКА"
	d.Wide = true
	v := newDomainView(rep)
	hosts := sortHosts(rep)
	counts := map[string]int{}
	for _, h := range hosts {
		counts[hostGroup(rep, h)]++
	}
	for _, h := range hosts {
		row := v.row(h)
		g := hostGroup(rep, h)
		row.Group = g + " \u00b7 " + strconv.Itoa(counts[g])
		d.Rows = append(d.Rows, row)
	}
	if len(rep.Related) > 0 {
		d.Foot = append(d.Foot, ui.Dim("в тех же сертификатах: "+strings.Join(cut(rep.Related, 8), " ")))
	}
	for _, n := range append(sourceLines(rep.Sources, false), rep.Notes...) {
		d.Foot = append(d.Foot, ui.Dim(n))
	}
	for _, e := range rep.Errors {
		d.Foot = append(d.Foot, ui.Warn("\u25b2 "+e))
	}
	d.Empty = "Ни одно имя не резолвится: домена нет, он не делегирован или DNS недоступен"
	return d
}

// hostGroup: беда, сам домен или остальные имена.
func hostGroup(rep *domaudit.Report, h domaudit.Host) string {
	switch {
	case h.Takeover != nil || h.Silent() || h.Internal || h.Status >= 400 || h.CertError != "":
		return "ВНИМАНИЕ"
	case h.Name == rep.Domain:
		return "САМ ДОМЕН"
	}
	return "ИМЕНА"
}

func mailWord(l level.Level) string {
	switch l {
	case level.Bad:
		return "плохо"
	case level.Warn:
		return "неполно"
	case level.OK:
		return "в порядке"
	}
	return "-"
}

// domainLamps: сайт, сертификат, регистрация и почта домена - по лампе на каждое.
func domainLamps(domain string) []string {
	site := health.Site("https://" + domain)
	cert := health.Cert(domain)
	lamps := []string{level.Lamp(site.Level, "сайт "+site.Text), level.Lamp(cert.Level, "сертификат "+cert.Text)}
	if cert.Level >= level.Warn {
		lamps[1] += ui.Dim("  причина - вкладка Сертификат")
	}
	if reg, err := inspect.Registration(domain); err == nil && !reg.Expires.IsZero() {
		c := health.Deadline("домен", reg.Expires, health.DomainWarnDays, health.DomainBadDays)
		lamp := level.Lamp(c.Level, "домен "+c.Text) + ui.Dim("  "+reg.Registrar)
		if c.Level >= level.Warn {
			lamp += ui.Dim(" - продлить там, лучше с автопродлением")
		}
		lamps = append(lamps, lamp)
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
		lamps = append(lamps, level.Lamp(level.Warn, "почта: "+strings.Join(reasons, ", "))+ui.Dim("  что сделать - вкладка Почта"))
	default:
		lamps = append(lamps, level.Lamp(level.Bad, "почта: "+strings.Join(reasons, ", "))+ui.Dim("  письма могут уходить в спам, что сделать - вкладка Почта"))
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
	lvl, word, note := hostState(h)
	if note == "" {
		note = v.note(h, ip)
		if h.Engine != "" {
			note = strings.TrimSuffix(h.Engine+" · "+note, " · ")
		}
	}
	row.Cells = ui.Fit(h.Name, 29) + " " + ui.Pad(level.Lamp(lvl, word), 12) + ui.Dim(note)
	for _, ip := range h.IPs {
		line := ui.Pad(ip, 17) + " "
		if a, ok := v.addrs[ip]; ok && a.Internal {
			line += ui.Dim("внутренний адрес")
		} else if ok {
			line += ui.Dim(strings.TrimSpace(ownerText(a)+" "+placeText(a)) + " AS" + a.AS.ASN)
			if a.PTR != "" {
				line += ui.Dim("  " + a.PTR)
			}
		}
		row.Pane = append(row.Pane, line)
	}
	var about []string
	for _, t := range []string{h.Engine, h.Server, h.Title} {
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
	if h.CertName != "" {
		row.Pane = append(row.Pane, ui.Dim("сертификат на "+h.CertName))
	}
	switch {
	case h.Takeover != nil:
		row.Pane = append(row.Pane, ui.Bad("можно захватить через "+h.Takeover.Service+": CNAME "+h.Takeover.Target+", "+h.Takeover.Why),
			ui.Dim("\u2192 убрать CNAME в DNS или привязать имя в "+h.Takeover.Service))
	case h.Status == 0 && h.CNAME != "" && len(h.IPs) == 0:
		row.Pane = append(row.Pane, ui.Warn("CNAME на "+h.CNAME+", а такого имени нет: запись устарела"),
			ui.Dim("\u2192 удалить CNAME "+h.Name+" в DNS - висячую запись могут захватить"))
	case h.Internal:
		row.Pane = append(row.Pane, ui.Warn("имя ведёт на внутренний адрес: снаружи за ним никого, зато видно устройство сети"),
			ui.Dim("\u2192 убрать "+h.Name+" из общего DNS и держать во внутреннем"))
	case h.Silent():
		row.Pane = append(row.Pane, ui.Warn("имя есть в DNS или логах сертификатов, а сайта нет: запись устарела?"),
			ui.Dim("\u2192 имя не нужно - удалить запись в DNS; нужно - поднять сайт или редирект"))
	case h.CertError != "":
		row.Pane = append(row.Pane, ui.Warn("браузер не пустит: сертификат "+h.CertError),
			ui.Dim("\u2192 выпустить сертификат на "+h.Name+" на сервере или в панели хостинга; имя не нужно - удалить запись в DNS"))
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
		return strings.TrimSpace(ownerText(a) + " " + placeText(a))
	case v.apex[ip]:
		return "тот же сервер"
	case a.CDN:
		return "за CDN " + ownerText(a)
	}
	return "другой сервер: " + strings.TrimSpace(ownerText(a)+" "+placeText(a))
}

// hostState: ответ сайта - уровень и слово; у молчащих и сломанных сразу и заметка.
func hostState(h domaudit.Host) (l level.Level, word, note string) {
	switch {
	case h.Takeover != nil:
		return level.Bad, "захват", "CNAME " + h.Takeover.Target + ": " + h.Takeover.Why
	case h.Status == 0 && h.CNAME != "" && len(h.IPs) == 0:
		return level.Warn, "в никуда", "CNAME " + h.CNAME + " не резолвится"
	case h.Internal:
		return level.Warn, "внутри", "внутренний адрес " + strings.Join(h.IPs, ", ")
	case h.Unchecked:
		return level.Skip, "", "по HTTP не спрашивали: имён больше предела"
	case h.Status == 0:
		return level.Bad, "молчит", "не отвечает по HTTP"
	case h.Status >= 500:
		return level.Bad, strconv.Itoa(h.Status), "сервер отвечает ошибкой"
	case h.CertError != "":
		return level.Warn, strconv.Itoa(h.Status), "сертификат: " + h.CertError
	case h.Status >= 400:
		return level.Warn, strconv.Itoa(h.Status), ""
	}
	return level.OK, strconv.Itoa(h.Status), ""
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
		case h.Silent() || h.Internal || h.Status >= 500 || h.CertError != "":
			return 2
		case h.Status >= 400:
			return 1
		}
		return 0
	}
	sort.SliceStable(hosts, func(i, j int) bool { return rank(hosts[i]) > rank(hosts[j]) })
	return hosts
}
