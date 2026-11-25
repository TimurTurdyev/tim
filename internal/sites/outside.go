package sites

import (
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/TimurTurdyev/tim/internal/app"
	"github.com/TimurTurdyev/tim/internal/baseline"
	"github.com/TimurTurdyev/tim/internal/domaudit"
	"github.com/TimurTurdyev/tim/internal/portscan"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// Что у домена видно снаружи сравнивается с прошлым разом: новое живое имя и новый порт своего сервера - повод
// вспомнить, кто их открыл. Виды source и addr служебные: по ним видно, что новое только для нас.
const (
	kindHost   = "host"
	kindPort   = "port"
	kindSource = "source"
	kindAddr   = "addr"

	checkedMark = " проверен" // у адреса, чьи порты проверили сами
)

// watchedSources: подписи без своей строки в источниках, за которыми следим: эти имена видит любой сбор.
var watchedSources = []string{"апекс", "MX", "NS"}

func outsideObject(domain string) string { return "outside-" + domain }

// watchOutside: ночное наблюдение; обходятся домены из sites, их порты проверяем сами.
func watchOutside(env *app.Env, domain string) {
	opt := domaudit.Options{Scan: true, Direct: directIface(env.Cfg, false), Inside: insideOf(env, domain)}
	observeOutside(domaudit.Names(domain, opt), time.Now())
}

// observeOutside сравнивает отчёт с прошлым разом; что стало видно только сейчас, но не новое, сразу своё.
func observeOutside(rep *domaudit.Report, now time.Time) {
	if len(rep.Hosts) == 0 {
		return // DNS не ответил: это не "всё пропало"
	}
	object := outsideObject(rep.Domain)
	res := baseline.Observe(object, outsideSnapshot(rep), now)
	_ = baseline.AcceptAll(object, familiar(rep, res.New, now))
}

func outsideSnapshot(rep *domaudit.Report) baseline.Snapshot {
	snap := baseline.Snapshot{}
	snap[kindHost], snap[kindSource] = namesSnapshot(rep)
	scanned := map[string]bool{}
	var ports, addrs []baseline.Item
	for _, a := range rep.Addrs {
		if !a.Checked && (!a.Scanned || !rep.PortsKnown()) {
			continue
		}
		scanned[a.IP] = true
		// Проверенное самими и данные Shodan видят разное: смена источника - не новый порт.
		key := a.IP
		if a.Checked {
			key += checkedMark
		}
		addrs = append(addrs, baseline.Item{Key: key})
		for _, p := range a.Ports() {
			if slices.ContainsFunc(a.Open, func(s portscan.Service) bool { return s.Port == p && s.Silent }) {
				continue // молчит: за VPN такой не виден, и порт мигал бы
			}
			key := a.IP + ":" + strconv.Itoa(p)
			ports = append(ports, baseline.Item{Key: key, Text: key + " · " + strings.Join(cut(a.Hosts, 2), ", ")})
		}
	}
	if len(scanned) == 0 {
		return snap
	}
	snap[kindPort], snap[kindAddr] = append([]baseline.Item{}, ports...), addrs
	// Про адрес сегодня не спрашивали: его неотмеченные порты остаются ждать.
	for _, it := range baseline.Pending(outsideObject(rep.Domain)) {
		if ip, _, _ := strings.Cut(it.Key, ":"); it.Kind == kindPort && !scanned[ip] {
			snap[kindPort] = append(snap[kindPort], baseline.Item{Key: it.Key, Text: it.Text})
		}
	}
	return snap
}

// namesSnapshot: имена и источники, о которых сегодня можно судить. Свой источник с неполным проходом и память
// прошлых сборов судить не дают: имя, которое знают только они, не становится ни новым, ни своим - его оценит
// первый полный проход. Имя, которое уже ждёт отметки, остаётся ждать.
func namesSnapshot(rep *domaudit.Report) (hosts, sources []baseline.Item) {
	hosts, sources = []baseline.Item{}, []baseline.Item{}
	waiting := map[string]bool{}
	for _, it := range baseline.Pending(outsideObject(rep.Domain)) {
		waiting[it.Key] = it.Kind == kindHost
	}
	weak := map[string]bool{}
	for _, s := range rep.Sources {
		weak[s.Name] = s.Own && s.Cut
	}
	ran := map[string]bool{} // источники, которые назвали живое имя сами, а не по памяти
	for _, h := range rep.Hosts {
		if len(h.IPs) == 0 {
			continue
		}
		found := foundNow(h)
		for _, s := range found {
			ran[s] = true
		}
		if slices.Contains(watchedSources, h.Source) || waiting[h.Name] || slices.ContainsFunc(found, func(s string) bool { return !weak[s] }) {
			hosts = append(hosts, baseline.Item{Key: h.Name, Text: h.Name + " · " + moreText(h.IPs) + " · " + h.Source})
		}
	}
	for _, s := range rep.Sources {
		// Знакомым свой источник становится, когда прошёл всё и сам назвал живое имя.
		if s.Names > 0 && (!s.Own || !s.Cut && ran[s.Name]) {
			sources = append(sources, baseline.Item{Key: sourceKey(s)})
		}
	}
	return hosts, sources
}

// foundNow: источники, которые назвали имя в этом сборе, а не помнят его с прошлого.
func foundNow(h domaudit.Host) []string {
	var out []string
	for _, s := range h.Sources {
		if !slices.Contains(h.Recalled, s) {
			out = append(out, s)
		}
	}
	return out
}

// sourceKey: источник с выпуском данных и охватом - после смены словаря, архива или набора поддоменов, по
// которым он искал, источник снова "ответил впервые".
func sourceKey(s domaudit.SourceState) string {
	return strings.TrimSpace(s.Name + " " + strings.TrimSpace(s.Version+" "+s.Scope))
}

// familiar: что из нового не новое - имена, которые принёс впервые ответивший или урезанный источник, и порты
// адреса, которого раньше не было; служебные виды - всегда.
func familiar(rep *domaudit.Report, fresh []baseline.Item, now time.Time) []baseline.Item {
	trusted := map[string]bool{} // источники, по которым новое имя - и правда новое
	keys := map[string]string{}
	for _, s := range rep.Sources {
		trusted[s.Name], keys[sourceKey(s)] = !s.Cut, s.Name
	}
	firstSeen := map[string]bool{} // адреса, которых раньше не было
	for _, it := range fresh {
		if !it.Seen.Equal(now) {
			continue
		}
		switch it.Kind {
		case kindSource:
			trusted[keys[it.Key]] = false
		case kindAddr:
			firstSeen[strings.TrimSuffix(it.Key, checkedMark)] = true
		}
	}
	sources, watched := map[string][]string{}, map[string]bool{}
	for _, h := range rep.Hosts {
		sources[h.Name], watched[h.Name] = foundNow(h), slices.Contains(watchedSources, h.Source)
	}
	var out []baseline.Item
	for _, it := range fresh {
		own := false
		switch it.Kind {
		case kindSource, kindAddr:
			own = true
		case kindHost:
			// Запись MX или NS читается целиком в любом сборе: имя из неё новое, кто бы ещё его ни назвал.
			known := sources[it.Key]
			own = it.Seen.Equal(now) && !watched[it.Key] && len(known) > 0 &&
				!slices.ContainsFunc(known, func(s string) bool { return trusted[s] })
		case kindPort:
			ip, _, _ := strings.Cut(it.Key, ":")
			own = it.Seen.Equal(now) && firstSeen[ip]
		}
		if own {
			out = append(out, it)
		}
	}
	return out
}

// outsideNew: неотмеченное новое снаружи - имена и порты по адресам.
func outsideNew(domain string) (hosts map[string]baseline.Item, ports map[string][]baseline.Item) {
	hosts, ports = map[string]baseline.Item{}, map[string][]baseline.Item{}
	for _, it := range baseline.Pending(outsideObject(domain)) {
		switch it.Kind {
		case kindHost:
			hosts[it.Key] = it
		case kindPort:
			ip, _, _ := strings.Cut(it.Key, ":")
			ports[ip] = append(ports[ip], it)
		}
	}
	return hosts, ports
}

// appearedText: когда новое стало видно.
func appearedText(it baseline.Item) string {
	if it.After.IsZero() {
		return "замечено " + it.Seen.Local().Format("02.01 15:04")
	}
	return "появилось между " + it.After.Local().Format("02.01 15:04") + " и " + it.Seen.Local().Format("02.01 15:04")
}

// outsideNews: то же словами - для главного экрана и уведомления.
func outsideNews(domain string) []string {
	hosts, ports := outsideNew(domain)
	var names, opened []string
	for n := range hosts {
		names = append(names, n)
	}
	for ip, items := range ports {
		for _, it := range items {
			opened = append(opened, strings.TrimPrefix(it.Key, ip+":")+" на "+ip)
		}
	}
	slices.Sort(names)
	slices.Sort(opened)
	var out []string
	switch {
	case len(names) == 1:
		out = append(out, "новое имя "+names[0])
	case len(names) > 1:
		out = append(out, ui.Count(len(names), "новое имя", "новых имени", "новых имён")+": "+strings.Join(cut(names, 3), ", "))
	}
	switch {
	case len(opened) == 1:
		out = append(out, "снаружи виден новый порт "+opened[0])
	case len(opened) > 1:
		out = append(out, "снаружи видны новые порты: "+strings.Join(cut(opened, 3), ", "))
	}
	return out
}
