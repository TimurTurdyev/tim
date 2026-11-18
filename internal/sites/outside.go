package sites

import (
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/TimurTurdyev/tim/internal/baseline"
	"github.com/TimurTurdyev/tim/internal/domaudit"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// Что у домена видно снаружи сравнивается с прошлым разом: новое живое имя и новый порт своего сервера - повод
// вспомнить, кто их открыл. Виды source и addr служебные: по ним видно, что новое только для нас.
const (
	kindHost   = "host"
	kindPort   = "port"
	kindSource = "source"
	kindAddr   = "addr"
)

// watchedSources: имена не из источников, за которыми следим; остальное видит только полный сбор, и ночью оно
// выглядело бы пропавшим.
var watchedSources = []string{"апекс", "перебор", "MX", "NS"}

func outsideObject(domain string) string { return "outside-" + domain }

// watchOutside: ночное наблюдение.
func watchOutside(domain string) { observeOutside(domaudit.Names(domain), time.Now()) }

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
		if s.Names > 0 {
			snap[kindSource] = append(snap[kindSource], baseline.Item{Key: s.Name})
		}
	}
	if !rep.PortsKnown() {
		return snap
	}
	snap[kindPort], snap[kindAddr] = []baseline.Item{}, []baseline.Item{}
	scanned := map[string]bool{}
	for _, a := range rep.Addrs {
		if !a.Scanned {
			continue
		}
		scanned[a.IP] = true
		snap[kindAddr] = append(snap[kindAddr], baseline.Item{Key: a.IP})
		if a.Services == nil {
			continue
		}
		for _, p := range a.Services.Ports {
			key := a.IP + ":" + strconv.Itoa(p)
			snap[kindPort] = append(snap[kindPort], baseline.Item{Key: key, Text: key + " · " + strings.Join(cut(a.Hosts, 2), ", ")})
		}
	}
	// Про адрес сегодня не спрашивали: его неотмеченные порты остаются ждать.
	for _, it := range baseline.Pending(outsideObject(rep.Domain)) {
		if ip, _, _ := strings.Cut(it.Key, ":"); it.Kind == kindPort && !scanned[ip] {
			snap[kindPort] = append(snap[kindPort], baseline.Item{Key: it.Key, Text: it.Text})
		}
	}
	return snap
}

// familiar: что из нового не новое - имена, которые принёс впервые ответивший или урезанный источник, и порты
// адреса, которого раньше не было; служебные виды - всегда.
func familiar(rep *domaudit.Report, fresh []baseline.Item, now time.Time) []baseline.Item {
	trusted := map[string]bool{} // источники, по которым новое имя - и правда новое
	for _, s := range rep.Sources {
		trusted[s.Name] = !s.Cut
	}
	firstSeen := map[string]bool{} // адреса, которых раньше не было
	for _, it := range fresh {
		if !it.Seen.Equal(now) {
			continue
		}
		switch it.Kind {
		case kindSource:
			trusted[it.Key] = false
		case kindAddr:
			firstSeen[it.Key] = true
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
