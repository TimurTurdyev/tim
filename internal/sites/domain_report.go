package sites

import (
	"cmp"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/TimurTurdyev/tim/internal/domaudit"
	"github.com/TimurTurdyev/tim/internal/health"
	"github.com/TimurTurdyev/tim/internal/inspect"
	"github.com/TimurTurdyev/tim/internal/level"
	"github.com/TimurTurdyev/tim/internal/par"
	"github.com/TimurTurdyev/tim/internal/ui"
	"github.com/TimurTurdyev/tim/report"
)

// Таблицы view не режет по ширине, поэтому длинное режется здесь.
const (
	ownerWidth = 30
	nameWidth  = 44
	whatWidth  = 100
)

// domainAbout: регистрация и почта домена; nil - не узнали.
// registration: quiet - без посредника rdap.org.
func registration(domain string, quiet bool) (*inspect.Domain, error) {
	if quiet {
		return health.RegistrationOwn(inspect.Registrable(domain))
	}
	return health.Registration(inspect.Registrable(domain))
}

type domainAbout struct {
	reg  *inspect.Domain
	mail *inspect.Mail
}

// aboutDomain: регистрация и почта домена; quiet - регистрацию спрашиваем только у реестра зоны.
func aboutDomain(domain string, quiet bool) domainAbout {
	var a domainAbout
	par.All(func() {
		if d, err := registration(domain, quiet); err == nil {
			a.reg = d
		}
	}, func() { a.mail = inspect.MailCheck(domain, inspect.DKIMSelectors) })
	return a
}

// domainReport: всё о домене блоками; Data - то же, что уходит в --json.
func domainReport(rep *domaudit.Report, about domainAbout) report.Report {
	r := report.Report{Schema: "tim.domain/1", Title: rep.Domain, Data: rep}
	found := domainFindings(rep)
	if len(found) > 0 {
		worst := level.Skip
		for _, f := range found {
			worst = level.Worst(worst, f.Level)
		}
		r.Verdict = report.Verdict{Level: worst, Text: ui.Count(len(found), "замечание", "замечания", "замечаний")}
	}
	if f := domainFacts(rep, about); len(f) > 0 {
		r.Blocks = append(r.Blocks, f)
	}
	if len(rep.Hosts) > 0 {
		r.Blocks = append(r.Blocks, report.Heading("Сети"), networksTable(rep), report.Heading("Имена"), namesTable(rep))
	}
	if t := portsTable(rep); len(t.Rows) > 0 {
		r.Blocks = append(r.Blocks, report.Heading("Порты наружу"), t, report.Note(portsNote(rep)))
	}
	if near := neighborFacts(rep); len(near) > 0 {
		r.Blocks = append(r.Blocks, report.Heading("Соседи по адресу"), near)
	}
	if news := outsideNews(rep.Domain); len(news) > 0 {
		r.Blocks = append(r.Blocks, report.Heading("Новое снаружи"), report.Facts{{Key: "с прошлого раза", Value: strings.Join(news, ", "), Level: level.Warn}},
			report.Note("своё отметить: space на экране tim domain "+rep.Domain))
	}
	if len(found) > 0 {
		r.Blocks = append(r.Blocks, report.Heading("Что не так"), found)
	}
	r.Blocks = append(r.Blocks, report.Heading("Откуда"))
	for _, n := range domainNotes(rep) {
		r.Blocks = append(r.Blocks, report.Note(n))
	}
	if len(rep.Hosts) > 0 {
		r.Blocks = append(r.Blocks, report.Next{{Text: "карта и таблицы в браузере", Command: "tim domain " + rep.Domain + " --map"}})
	}
	return r
}

func domainFacts(rep *domaudit.Report, about domainAbout) report.Facts {
	var f report.Facts
	add := func(key, value string, l level.Level) { f = append(f, report.Fact{Key: key, Value: value, Level: l}) }
	if d := about.reg; d != nil {
		add("регистратор", ui.Dash(d.Registrar), level.Skip)
		if !d.Expires.IsZero() {
			text, l := deadline(d.Expires, time.Now())
			add("оплачен до", text, l)
		}
	}
	addrs := addrIndex(rep)
	for _, s := range rep.NS {
		add("NS", serverText(s, addrs), level.Skip)
	}
	for _, s := range rep.MX {
		add("MX", serverText(s, addrs), level.Skip)
	}
	if m := about.mail; m != nil && len(rep.Hosts) > 0 {
		text, l := mailText(m)
		add("почта", text, l)
	}
	for _, t := range rep.TXT {
		add("TXT", t, level.Info)
	}
	if len(rep.Related) > 0 {
		add("в сертификатах", strings.Join(cut(rep.Related, 10), ", "), level.Skip)
	}
	if len(rep.Trackers) > 0 {
		var t []string
		for _, x := range rep.Trackers {
			t = append(t, x.Kind+" "+x.ID)
		}
		add("счётчики", strings.Join(t, ", "), level.Skip)
	}
	return f
}

func deadline(t, now time.Time) (string, level.Level) {
	days := ui.DaysUntil(t, now)
	date := t.Format("2006-01-02")
	switch {
	case days <= 0:
		return date + ", " + ui.Expired(days), level.Bad
	case days < 14:
		return fmt.Sprintf("%s, осталось %d дн", date, days), level.Warn
	}
	return fmt.Sprintf("%s, осталось %d дн", date, days), level.Skip
}

// mailText: почта одной строкой - подробно её показывает tim mail.
func mailText(m *inspect.Mail) (string, level.Level) {
	verdict, reasons := m.Verdict()
	switch {
	case m.NullMX:
		return "домен её не принимает (null MX)", level.Info
	case verdict == level.Skip:
		return "не проверено: " + strings.Join(reasons, ", "), level.Info
	case verdict == level.OK:
		return "настроена верно", level.OK
	}
	return strings.Join(reasons, ", ") + " - подробно в tim mail", verdict
}

func addrIndex(rep *domaudit.Report) map[string]domaudit.Addr {
	out := map[string]domaudit.Addr{}
	for _, a := range rep.Addrs {
		out[a.IP] = a
	}
	return out
}

// serverText: сервер почты или имён - имя, адреса, чья сеть и где стоит.
func serverText(s domaudit.Server, addrs map[string]domaudit.Addr) string {
	parts := []string{s.Name}
	if s.Pref > 0 {
		parts[0] += " " + strconv.Itoa(s.Pref)
	}
	if len(s.IPs) == 0 {
		return parts[0] + " · адреса нет"
	}
	parts = append(parts, moreText(s.IPs))
	if a, ok := addrs[s.IPs[0]]; ok {
		if owner := ownerText(a); owner != "" {
			parts = append(parts, owner)
		}
		if place := placeText(a); place != "" {
			parts = append(parts, place)
		}
	}
	return strings.Join(parts, " · ")
}

// moreText: первое значение и сколько ещё - "192.0.2.1 +2".
func moreText(list []string) string {
	switch len(list) {
	case 0:
		return ""
	case 1:
		return list[0]
	}
	return list[0] + " +" + strconv.Itoa(len(list)-1)
}

func ownerText(a domaudit.Addr) string {
	if a.Internal {
		return "внутренний адрес"
	}
	return a.AS.Owner()
}

// placeText: где стоит адрес; узел CDN отвечает отовсюду.
func placeText(a domaudit.Addr) string {
	if a.CDN {
		return "везде"
	}
	return a.Country
}

func networksTable(rep *domaudit.Report) report.Table {
	t := report.Table{Columns: []report.Column{{Title: "ВЛАДЕЛЕЦ", Style: report.Key}, {Title: "AS"}, {Title: "ПОДСЕТЬ", Style: report.Muted},
		{Title: "ГДЕ"}, {Title: "АДРЕСОВ", Kind: report.Number}, {Title: "ИМЁН", Kind: report.Number}, {Title: "ЧТО", Style: report.Muted}}}
	for _, n := range domaudit.Networks(rep) {
		as := ""
		if n.ASN != "" {
			as = "AS" + n.ASN
		}
		t.Rows = append(t.Rows, []any{ui.Clip(networkName(n), ownerWidth), as, moreText(n.Prefixes), networkPlace(n), n.Addrs, n.Hosts, networkRole(n)})
	}
	return t
}

func networkName(n domaudit.Network) string {
	switch {
	case n.Internal:
		return "внутренние адреса"
	case n.Owner == "":
		return "неизвестно чья"
	}
	return n.Owner
}

func networkPlace(n domaudit.Network) string {
	if n.CDN {
		return "везде"
	}
	if len(n.Countries) > 3 {
		return strings.Join(n.Countries[:3], " ") + " +" + strconv.Itoa(len(n.Countries)-3)
	}
	return strings.Join(n.Countries, " ")
}

func networkRole(n domaudit.Network) string {
	var out []string
	for _, r := range []struct {
		on   bool
		word string
	}{{n.CDN, "CDN"}, {n.MX, "почта"}, {n.NS, "DNS"}} {
		if r.on {
			out = append(out, r.word)
		}
	}
	return strings.Join(out, ", ")
}

func namesTable(rep *domaudit.Report) report.Table {
	t := report.Table{Columns: []report.Column{{Title: "ИМЯ", Style: report.Key}, {Title: "ОТВЕТ"}, {Title: "АДРЕС"}, {Title: "СЕТЬ", Style: report.Muted},
		{Title: "ЧТО ТАМ", Style: report.Muted}}}
	addrs := addrIndex(rep)
	for _, h := range sortHosts(rep) {
		owner := ""
		if len(h.IPs) > 0 {
			owner = ownerText(addrs[h.IPs[0]])
		}
		l, word, note := hostState(h)
		t.Rows = append(t.Rows, []any{ui.Clip(h.Name, nameWidth), report.Mark{Text: word, Level: l}, moreText(h.IPs), ui.Clip(owner, ownerWidth),
			ui.Clip(hostWhat(h, note), whatWidth)})
	}
	return t
}

// hostWhat: что на имени - беда, движок или сервер, куда ведёт, заголовок; у молчащего - откуда имя.
func hostWhat(h domaudit.Host, note string) string {
	final, from := "", ""
	if h.Final != "" {
		final = "-> " + h.Final
	}
	if h.Status == 0 {
		from = "имя: " + h.Source
	}
	var parts []string
	for _, p := range []string{note, cmp.Or(h.Engine, h.Server), final, h.Title, from} {
		if p != "" {
			parts = append(parts, p)
		}
	}
	return strings.Join(parts, " · ")
}

func portsTable(rep *domaudit.Report) report.Table {
	t := report.Table{Columns: []report.Column{{Title: "АДРЕС", Style: report.Key}, {Title: "ПОРТЫ"}, {Title: "ЧТО ОТВЕЧАЕТ", Style: report.Muted}, {Title: "CVE", Kind: report.Number}}}
	for _, a := range rep.Addrs {
		ports := a.Ports()
		if len(ports) == 0 {
			continue
		}
		cve := 0
		if a.Services != nil {
			cve = len(a.Services.CVE)
		}
		t.Rows = append(t.Rows, []any{a.IP, ui.Clip(portList(ports), 40), ui.Clip(strings.Join(a.Answers(), ", "), whatWidth), cve})
	}
	return t
}

func portList(ports []int) string {
	out := make([]string, len(ports))
	for i, p := range ports {
		out[i] = strconv.Itoa(p)
	}
	return strings.Join(out, " ")
}

// portsNote: откуда порты; уязвимости - догадка по номеру версии, поэтому счётчиком и с оговоркой.
func portsNote(rep *domaudit.Report) string {
	checked, shodan, cve := false, false, false
	for _, a := range rep.Addrs {
		checked = checked || a.Checked
		shodan = shodan || a.Services != nil && !a.Checked
		cve = cve || a.Services != nil && len(a.Services.CVE) > 0
	}
	var parts []string
	if checked {
		parts = append(parts, "порты проверены сейчас, по ответу сервиса")
	}
	if shodan {
		parts = append(parts, "порты и версии - по данным Shodan, до недели назад (проверить самим: --scan)")
	}
	if cve {
		parts = append(parts, "CVE подобраны по номеру версии: Debian и Ubuntu чинят, не меняя номер")
	}
	return strings.Join(parts, "; ")
}

// portFindings: сервисы, которым не место в интернете, - по ответу самого сервиса.
func portFindings(rep *domaudit.Report) report.Findings {
	var out report.Findings
	for _, a := range rep.Addrs {
		for _, s := range a.Open {
			why := s.Risk()
			if why == "" || s.Silent {
				continue
			}
			out = append(out, report.Finding{Area: "порты", Level: level.Bad, Subject: a.IP, Intrusive: true,
				Text: fmt.Sprintf("наружу отвечает %s на %s:%d", strings.TrimSpace(s.Name+" "+s.Product), a.IP, s.Port), Why: why,
				Fix: fmt.Sprintf("слушать на 127.0.0.1 или закрыть в файрволе, снаружи ходить через ssh-туннель: tim tunnel port %d <сервер>", s.Port)})
		}
	}
	return out
}

func neighborFacts(rep *domaudit.Report) report.Facts {
	var f report.Facts
	for _, a := range rep.Addrs {
		if len(a.Shared) > 0 {
			f = append(f, report.Fact{Key: a.IP, Value: neighborText(a, 8)})
		}
	}
	return f
}

func neighborText(a domaudit.Addr, n int) string {
	if len(a.Shared) == 0 {
		return "других сайтов на адресе не нашлось"
	}
	text := strings.Join(cut(a.Shared, n), ", ")
	if a.SharedSeen > len(a.Shared) {
		text += fmt.Sprintf(" (всего на адресе видели %d)", a.SharedSeen)
	}
	return text
}

// domainFindings: что стоит исправить в DNS.
func domainFindings(rep *domaudit.Report) report.Findings {
	if len(rep.Hosts) == 0 {
		return report.Findings{{Area: "dns", Level: level.Bad, Text: "ни одно имя не резолвится: домена нет, он не делегирован или DNS недоступен",
			Fix: "проверить, нет ли опечатки, и записи у регистратора: tim site " + rep.Domain}}
	}
	out := portFindings(rep)
	var badCert []string
	for _, h := range rep.Hosts {
		switch {
		case h.Takeover != nil:
			out = append(out, report.Finding{Area: "захват", Level: level.Bad, Subject: h.Name, Text: h.Name + " можно захватить через " + h.Takeover.Service,
				Why: "CNAME " + h.Takeover.Target + ", " + h.Takeover.Why, Fix: "убрать CNAME в DNS или привязать имя в " + h.Takeover.Service})
		case h.Status == 0 && h.CNAME != "" && len(h.IPs) == 0:
			out = append(out, report.Finding{Area: "dns", Level: level.Warn, Subject: h.Name, Text: h.Name + " ведёт в никуда",
				Why: "CNAME на " + h.CNAME + ", а такого имени нет: висячую запись могут захватить", Fix: "удалить CNAME " + h.Name + " в DNS"})
		case h.Internal:
			out = append(out, report.Finding{Area: "dns", Level: level.Warn, Subject: h.Name, Text: h.Name + " ведёт на внутренний адрес " + strings.Join(h.IPs, ", "),
				Why: "снаружи за ним никого, зато видно устройство сети", Fix: "убрать из общего DNS, держать во внутреннем"})
		case h.CertError != "":
			badCert = append(badCert, h.Name)
		}
	}
	if len(rep.InsideOnly) > 0 {
		out = append(out, report.Finding{Area: "сервер", Level: level.Info, Text: "на сервере настроены сайты, которых нет в DNS: " + strings.Join(cut(rep.InsideOnly, 5), ", "),
			Why: "сервер отвечает на эти имена, а записи у них нет: сайт забыт или запись ещё не завели", Fix: "убрать сайт с сервера или завести запись в DNS"})
	}
	if len(badCert) > 0 {
		out = append(out, report.Finding{Area: "сертификат", Level: level.Warn, Text: "браузер не пустит на https: " + strings.Join(cut(badCert, 5), ", "),
			Why: "сертификат чужой, просрочен или самоподписанный - причина у каждого имени в таблице", Fix: "выпустить сертификат на эти имена на сервере или в панели хостинга"})
	}
	slices.SortStableFunc(out, func(a, b report.Finding) int { return int(b.Level) - int(a.Level) })
	return out
}

// domainNotes: откуда имена, что осталось за пределами и кто не ответил.
func domainNotes(rep *domaudit.Report) []string {
	dead := 0
	for _, h := range rep.Hosts {
		if h.Silent() {
			dead++
		}
	}
	out := []string{fmt.Sprintf("имён %d, адресов %d, не отвечают по HTTP %d", len(rep.Hosts), len(rep.Addrs), dead)}
	out = append(out, sourceLines(rep, true)...)
	out = append(out, rep.Notes...)
	return append(out, rep.Errors...)
}
