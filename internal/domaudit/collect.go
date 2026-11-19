package domaudit

import (
	"cmp"
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

// Options: что собирать. Quiet - без сторонних сервисов с именами, соседями, местом и портами: остаются DNS,
// регистрация, владелец сети и запросы к самому сайту. Scan - проверить порты своих серверов самим.
type Options struct {
	Quiet         bool
	Scan          bool
	FreshCRT      bool
	SkipNeighbors bool
	Notify        func(string)

	night bool // ночной обход: без HTTP, соседей, места и медленных источников
}

// collector: сбор по кругам. Круг - новые имена: их адреса, чьи они, что отвечает сайт. Имена, которые всплыли по
// дороге, и ответы поздних источников идут во второй круг.
type collector struct {
	domain string
	opt    Options
	rep    *Report

	asking  *asking             // источники имён; nil - не спрашиваем
	extra   map[string]string   // имена не из источников: откуда
	known   map[string][]string // адреса, которые уже спросили
	wild    []string            // адреса несуществующего имени
	tried   map[string]string   // имя -> с какой подписью его резолвили
	dropped map[string]bool     // неотличимо от несуществующего
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
// от них берётся прошлый список. scan - порты проверить самим.
func Names(domain string, scan bool) *Report {
	c := newCollector(domain, Options{Scan: scan, night: true})
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
		extra: map[string]string{}, known: map[string][]string{}, tried: map[string]string{}, dropped: map[string]bool{},
		cnames: map[string]string{}, hosts: map[string]*Host{}, addrs: map[string]*Addr{}, group: par.NewGroup(probeWidth)}
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
		c.learn(n, fromBrute)
	}
	if c.asking != nil {
		c.asking.wait(fastWait)
	}
}

// learn запоминает имя домена, которое всплыло не из источников.
func (c *collector) learn(name, from string) {
	if subs, _ := splitNames([]string{name}, c.domain); len(subs) == 1 && c.extra[subs[0]] == "" {
		c.extra[subs[0]] = from
	}
}

// fresh: имена, которых ещё не резолвили; отброшенное из-за записи * возвращается, если нашёлся сертификат.
func (c *collector) fresh() map[string]string {
	out := map[string]string{}
	add := func(name, from string) {
		old, tried := c.tried[name]
		again := c.dropped[name] && from == fromCert && old != fromCert
		if out[name] == "" && (!tried || again) {
			out[name] = from
		}
	}
	add(c.domain, fromApex)
	if c.asking != nil {
		for n, from := range c.asking.found().label {
			add(n, from)
		}
	}
	for n, from := range c.extra {
		add(n, from)
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

// finish складывает отчёт: имена по алфавиту, адреса по порядку, подписи - по всем ответившим источникам.
func (c *collector) finish() {
	r := c.rep
	found := sourceNames{}
	if c.asking != nil {
		found = c.asking.found()
	}
	r.Related, r.Sources = found.related, found.states
	for _, name := range slices.Sorted(maps.Keys(c.hosts)) {
		h := *c.hosts[name]
		h.Sources = found.by[name]
		h.Source = cmp.Or(found.label[name], h.Source)
		if name == c.domain {
			h.Source = fromApex
		}
		r.Hosts = append(r.Hosts, h)
	}
	for _, ip := range slices.Sorted(maps.Keys(c.addrs)) {
		r.Addrs = append(r.Addrs, *c.addrs[ip])
	}
	failed := 0
	for _, s := range r.Sources {
		if s.Name == "crt.sh" {
			r.CRTCached = s.Cached
		}
		if s.Error != "" && s.Names == 0 {
			failed++
		}
	}
	if failed > 0 && failed == len(r.Sources) {
		c.fail("ни один источник имён не ответил: имена только из перебора частых")
	}
	if n := len(c.dropped); n > 0 {
		r.Notes = append(r.Notes, "у домена запись *."+c.domain+": отброшено имён, неотличимых от неё, - "+strconv.Itoa(n))
	}
	if c.probed > maxProbes {
		r.Notes = append(r.Notes, "по HTTP проверено "+strconv.Itoa(maxProbes)+" имён из "+strconv.Itoa(c.probed))
	}
	if c.scanned > maxScans {
		r.Notes = append(r.Notes, "порты спрошены у "+strconv.Itoa(maxScans)+" адресов из "+strconv.Itoa(c.scanned))
	}
	if c.met > maxNeighborAddrs {
		r.Notes = append(r.Notes, "соседи спрошены у "+strconv.Itoa(maxNeighborAddrs)+" адресов из "+strconv.Itoa(c.met))
	}
	if c.checked > maxChecks {
		r.Notes = append(r.Notes, "порты проверены у "+strconv.Itoa(maxChecks)+" адресов из "+strconv.Itoa(c.checked))
	}
	r.Trackers = homeTrackers(c.domain, r.Hosts)
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
