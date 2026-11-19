package domaudit

import (
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/TimurTurdyev/tim/internal/par"
)

const (
	fastWait = 4 * time.Second // сколько первый круг ждёт источники имён; поздние попадут во второй
	settle   = 2 * time.Second // за столько отвечает почти любой живой сайт: его сертификат успевает во второй круг
)

// Options: что собирать. Quiet - без сторонних сервисов: имена, соседи, место и порты только своими силами.
// Quick - свои источники на коротком сроке. Scan - проверить порты своих серверов самим. Direct - интерфейс, с
// которого запросы DNS идут мимо VPN; пусто - обычным путём.
type Options struct {
	Quiet         bool
	Quick         bool
	Scan          bool
	FreshCRT      bool
	SkipNeighbors bool
	Direct        string
	Notify        func(string)

	night bool // ночной обход: без HTTP, соседей, места и медленных источников
}

// collector: сбор по кругам. Круг - новые имена: их адреса, чьи они, что отвечает сайт. Имена, которые всплыли по
// дороге, и ответы поздних источников идут во второй круг.
type collector struct {
	domain string
	opt    Options
	rep    *Report

	asking  *asking                 // сторонние источники имён; nil - не спрашиваем
	from    map[string][]string     // имена, которые нашли сами: откуда
	states  map[string]*SourceState // что сказать о своих источниках
	related []string                // чужие домены, которые назвала сама цель
	known   map[string][]string     // адреса, которые уже спросили
	wild    []string                // адреса несуществующего имени
	tried   map[string]bool         // имя уже резолвили; true - тогда оно было точно заведённым
	dropped map[string]bool         // неотличимо от несуществующего
	cnames  map[string]string

	mu      sync.Mutex // имена пишут пробы, ошибки - кто первый наткнётся
	hosts   map[string]*Host
	addrs   map[string]*Addr
	group   *par.Group
	probing sync.WaitGroup // постановка проб в очередь
	pending atomic.Int32   // проб ещё идёт
	probed  int            // имён с адресами отдано на пробу: предел на оба круга
	scanned int            // своих адресов, у которых спросили порты, - тоже
	met     int            // и соседей
	checked int            // и чьи порты проверили сами
	alone   bool           // основной источник соседей не отвечает
	since   time.Time      // когда пошли первые пробы
}

// Collect собирает отчёт по домену из публичных источников: имена, их адреса и сети, почту и серверы имён, ответы
// сайтов, счётчики главной страницы и соседей по адресу.
func Collect(domain string, opt Options) *Report {
	c := newCollector(domain, opt)
	c.discover()
	c.round(c.servers())
	// Соседи первого круга - пока ждём поздние источники; у адресов, всплывших потом, - следом.
	// Порты - так же.
	first, ports := c.unmet(), c.unchecked()
	var met sync.WaitGroup
	met.Go(func() { c.neighbors(first) })
	met.Go(func() { c.checkPorts(ports) })
	c.surface()
	c.round(nil)
	met.Wait()
	c.neighbors(c.unmet())
	c.checkPorts(c.unchecked())
	c.probing.Wait()
	c.group.Wait()
	c.finish()
	return c.rep
}

// Names: живые имена домена, их адреса и порты своих - для ночного обхода: без HTTP, соседей и медленных источников,
// от них берётся прошлый список.
func Names(domain string, opt Options) *Report {
	opt.night = true
	c := newCollector(domain, opt)
	c.discover()
	c.asking.wait(sourceWait)
	_, addrs := c.resolve(c.fresh())
	maps.Copy(addrs, c.servers())
	c.enrich(addrs)
	c.checkPorts(c.unchecked())
	c.finish()
	return c.rep
}

func newCollector(domain string, opt Options) *collector {
	return &collector{domain: domain, opt: opt, rep: &Report{Domain: domain},
		from: map[string][]string{}, states: map[string]*SourceState{}, known: map[string][]string{}, tried: map[string]bool{},
		dropped: map[string]bool{},
		cnames:  map[string]string{}, hosts: map[string]*Host{}, addrs: map[string]*Addr{}, group: par.NewGroup(probeWidth)}
}

// discover: почта и серверы имён, перебор частых имён и быстрые источники.
func (c *collector) discover() {
	if !c.opt.Quiet {
		c.note(sourcesNote(stale(c.domain, c.opt)))
		c.asking = askSources(c.domain, c.opt)
	}
	var brute map[string][]string
	par.All(c.records, func() {
		c.wild = wildcard(c.domain)
		brute = bruteforce(c.domain, c.wild)
	})
	for n, ips := range brute {
		c.known[n] = ips
		c.learn(n, fromWords)
	}
	c.state(fromWords).Checked, c.state(fromWords).Total = len(Common), len(Common)
	if c.asking != nil {
		c.asking.wait(fastWait)
	}
}

// learn запоминает, что имя домена нашёл свой источник.
func (c *collector) learn(name, from string) {
	subs, _ := splitNames([]string{name}, c.domain)
	if len(subs) == 1 && !slices.Contains(c.from[subs[0]], from) {
		c.from[subs[0]] = append(c.from[subs[0]], from)
	}
}

// state: строка своего источника в отчёте.
func (c *collector) state(label string) *SourceState {
	if c.states[label] == nil {
		c.states[label] = &SourceState{Name: label, Own: true}
	}
	return c.states[label]
}

// fresh: имена, которых ещё не резолвили, со всеми подписями; отброшенное из-за записи * возвращается, если о нём
// узнал источник, который называет только заведённые имена.
func (c *collector) fresh() map[string][]string {
	all := map[string][]string{c.domain: {fromApex}}
	if c.asking != nil {
		for n, labels := range c.asking.found().labels {
			all[n] = append(all[n], labels...)
		}
	}
	for n, labels := range c.from {
		all[n] = append(all[n], labels...)
	}
	out := map[string][]string{}
	for n, labels := range all {
		wasFirm, tried := c.tried[n]
		if !tried || c.dropped[n] && firm(labels) && !wasFirm {
			out[n] = labels
		}
	}
	return out
}

// round: один круг. Пробы сайтов идут в фоне, адреса тем временем обрастают сведениями; also - адреса не от имён.
func (c *collector) round(also map[string]*Addr) {
	hosts, addrs := c.resolve(c.fresh())
	maps.Copy(addrs, also)
	c.probe(hosts)
	c.enrich(addrs)
}

// surface: всё, что успело всплыть после первого круга, - имена из сертификатов сайтов и поздние источники.
func (c *collector) surface() {
	if c.asking != nil {
		if late := c.asking.waiting(); len(late) > 0 {
			c.note("жду " + strings.Join(late, ", "))
		}
		c.asking.wait(sourceWait)
	}
	for c.pending.Load() > 0 && time.Since(c.since) < settle {
		time.Sleep(50 * time.Millisecond)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, h := range c.hosts {
		for _, n := range h.certNames {
			c.learn(n, fromSiteCert)
		}
	}
}

// finish складывает отчёт: имена по алфавиту, адреса по порядку, у каждого имени - кто его знает.
func (c *collector) finish() {
	r := c.rep
	found := sourceNames{}
	if c.asking != nil {
		found = c.asking.found()
	}
	r.Related = slices.Compact(slices.Sorted(slices.Values(slices.Concat(found.related, c.related))))
	for _, name := range slices.Sorted(maps.Keys(c.hosts)) {
		h := *c.hosts[name]
		labels := slices.Concat(c.from[name], found.labels[name])
		if name == c.domain {
			labels = append(labels, fromApex)
		}
		h.Source, h.Sources = best(labels), c.knownBy(name, found)
		if selfFound(labels) {
			r.Found.Own++
		} else {
			r.Found.Added++
		}
		r.Hosts = append(r.Hosts, h)
	}
	for _, ip := range slices.Sorted(maps.Keys(c.addrs)) {
		r.Addrs = append(r.Addrs, *c.addrs[ip])
	}
	r.Sources = append(c.ownStates(), found.states...)
	for _, s := range found.states {
		if s.Name == "crt.sh" {
			r.CRTCached = s.Cached
		}
		if s.Error != "" && s.Names == 0 {
			r.Found.Silent = append(r.Found.Silent, s.Name)
		}
	}
	r.Notes = append(r.Notes, c.limits()...)
	r.Trackers = homeTrackers(c.domain, r.Hosts)
}

// knownBy: источники, которые знают имя, - свои, о которых отчёт говорит строкой, и сторонние.
func (c *collector) knownBy(name string, found sourceNames) []string {
	var out []string
	for _, label := range reported {
		if slices.Contains(c.from[name], label) {
			out = append(out, label)
		}
	}
	return append(out, found.by[name]...)
}

// ownStates: строки своих источников - сколько имён назвал каждый; молчавший и без замечаний не показывается.
func (c *collector) ownStates() []SourceState {
	var out []SourceState
	for _, label := range reported {
		names := 0
		for _, labels := range c.from {
			if slices.Contains(labels, label) {
				names++
			}
		}
		st := c.states[label]
		if st == nil && names == 0 {
			continue
		}
		if st == nil {
			st = &SourceState{Name: label, Own: true}
		}
		st.Names = names
		out = append(out, *st)
	}
	return out
}

// limits: что осталось за пределами сбора.
func (c *collector) limits() []string {
	var out []string
	if n := len(c.dropped); n > 0 {
		out = append(out, "у домена запись *."+c.domain+": отброшено имён, неотличимых от неё, - "+strconv.Itoa(n))
	}
	for _, l := range []struct {
		got, max int
		text     string
	}{
		{c.probed, maxProbes, "по HTTP проверено %d имён из %d"},
		{c.scanned, maxScans, "порты спрошены у %d адресов из %d"},
		{c.met, maxNeighborAddrs, "соседи спрошены у %d адресов из %d"},
		{c.checked, maxChecks, "порты проверены у %d адресов из %d"},
	} {
		if l.got > l.max {
			out = append(out, fmt.Sprintf(l.text, l.max, l.got))
		}
	}
	return out
}

// fail: ошибка источника в отчёт - одна на всех.
func (c *collector) fail(text string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !slices.Contains(c.rep.Errors, text) {
		c.rep.Errors = append(c.rep.Errors, text)
	}
}

// say: замечание о сборе - одно на всех.
func (c *collector) say(text string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !slices.Contains(c.rep.Notes, text) {
		c.rep.Notes = append(c.rep.Notes, text)
	}
}

func (c *collector) note(s string) {
	if c.opt.Notify != nil {
		c.opt.Notify(s)
	}
}

func sourcesNote(asked []string) string {
	if len(asked) == 0 {
		return "имена из кэша, обновить: --fresh"
	}
	return "спрашиваю имена: " + strings.Join(asked, ", ")
}

// homeTrackers: счётчики с главной страницы домена.
func homeTrackers(domain string, hosts []Host) []Tracker {
	for _, h := range hosts {
		if (h.Name == domain || h.Name == "www."+domain) && len(h.trackers) > 0 {
			return h.trackers
		}
	}
	return nil
}
