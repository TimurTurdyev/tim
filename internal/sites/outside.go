package sites

import (
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/TimurTurdyev/tim/internal/baseline"
	"github.com/TimurTurdyev/tim/internal/config"
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
func watchOutside(cfg *config.Config, domain string) {
	observeOutside(domaudit.Names(domain, domaudit.Options{Scan: true, Direct: directIface(cfg, false)}), time.Now())
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
	snap := baseline.Snapshot{kindHost: {}, kindSource: {}}
	for _, h := range rep.Hosts {
		if len(h.IPs) > 0 && (len(h.Sources) > 0 || slices.Contains(watchedSources, h.Source)) {
			snap[kindHost] = append(snap[kindHost], baseline.Item{Key: h.Name, Text: h.Name + " · " + moreText(h.IPs) + " · " + h.Source})
		}
	}
	for _, s := range rep.Sources {
		// Свой источник, который не успел проверить всё, знакомым не становится: в полный проход он покажет
		// старое, о котором мы не знали.
		if cut := s.Own && s.Cut; s.Names > 0 && !cut {
			snap[kindSource] = append(snap[kindSource], baseline.Item{Key: sourceKey(s)})
		}
	}
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

// sourceKey: источник с выпуском его данных - после смены словаря или архива он снова "ответил впервые".
func sourceKey(s domaudit.SourceState) string {
	if s.Version == "" {
		return s.Name
	}
	return s.Name + " " + s.Version
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
	sources := map[string][]string{}
	for _, h := range rep.Hosts {
		sources[h.Name] = h.Sources
	}
	var out []baseline.Item
	for _, it := range fresh {
		own := false
		switch it.Kind {
		case kindSource, kindAddr:
			own = true
		case kindHost:
			known := sources[it.Key]
			own = it.Seen.Equal(now) && len(known) > 0 && !slices.ContainsFunc(known, func(s string) bool { return trusted[s] })
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
