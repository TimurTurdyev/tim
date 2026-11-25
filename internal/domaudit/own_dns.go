package domaudit

import (
	"errors"
	"net"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/TimurTurdyev/tim/internal/dnsq"
	"github.com/TimurTurdyev/tim/internal/inspect"
	"github.com/TimurTurdyev/tim/internal/namebase"
	"github.com/TimurTurdyev/tim/internal/paths"
	"github.com/TimurTurdyev/tim/internal/sweep"
	"github.com/TimurTurdyev/tim/internal/ui"
)

const (
	wordsFull  = 100000 // весь встроенный словарь
	wordsQuick = 20000
	sweepFull  = 20 * time.Second // столько же ждём сторонних: перебор идёт в это время
	sweepOwn   = 40 * time.Second // без сторонних ждать некого, зато словарь у серверов зоны проходит весь
	sweepQuick = 3 * time.Second
	sweepNight = time.Minute
	maxHits    = 3000             // больше имён словарь не находит и у огромных зон: дальше похоже на незамеченную запись *
	maxArchive = 10000            // больше имён в архиве только у площадок с чужими поддоменами
	wordsSlack = 10 * time.Second // поиск серверов зоны перед перебором

	// NoIface: мимо VPN просили, а физического интерфейса не нашлось.
	NoIface = "-"
)

// dnsPath: каким путём идёт массовая проверка имён; how - словами, для строки источника.
type dnsPath struct {
	opt    sweep.Options
	how    string
	zone   string // вершина зоны, чьи серверы спрашиваем
	direct bool   // спрашиваем сами серверы зоны
}

// found: что массовая проверка сказала об именах.
type found struct {
	live    []string // есть адрес или CNAME, и это не запись *
	firm    []string // отвечают как запись *, но заведены явно
	nodes   []string // имя есть, адреса у него нет
	zones   []string // дочерние зоны, куда отсылают серверы
	starred int      // имён, неотличимых от записи *
	checked int      // имён с ответом
	limited bool
	cut     bool // проверено не всё: срок, имена без ответа или запись *, о которой зона промолчала
}

// bind: запросы DNS с интерфейса мимо VPN, если так просили; не вышло - обычным путём и строкой в отчёте.
// Привязка общая на весь процесс: кто её поставил, тот в конце сбора и снимает.
func (c *collector) bind() {
	switch {
	case c.opt.Direct == "" || dnsq.Bound() == c.opt.Direct:
		return
	case c.opt.Direct == NoIface:
		c.say("мимо VPN не вышло: физический интерфейс не найден, запросы DNS идут обычным путём")
		return
	}
	if err := dnsq.Bind(c.opt.Direct); err != nil {
		c.say("мимо VPN через " + c.opt.Direct + " не вышло, запросы DNS идут обычным путём: " + ui.Plain(ui.ShortErr(err)))
		return
	}
	c.bound = true
}

// unbind: запросы DNS остального процесса снова идут обычным путём.
func (c *collector) unbind() {
	if c.bound {
		dnsq.Unbind()
		c.bound = false
	}
}

// path: серверы зоны, если до них можно достать, иначе резолверы - при перехвате порта 53 по DoH.
func (c *collector) path() dnsPath {
	c.pathOnce.Do(func() {
		if p, ok := zonePath(c.domain); ok {
			c.dns = p
			return
		}
		c.dns = dnsPath{opt: resolvers(), how: "через резолверы"}
		if dnsq.Hijacked() {
			c.dns.how = "через DoH: порт 53 перехвачен"
		}
	})
	return c.dns
}

// zonePath: путь к серверам зоны имени; ok=false - до них не достать.
func zonePath(name string) (dnsPath, bool) {
	zone, err := dnsq.FindZone(name)
	var servers []string
	for _, s := range zone.Servers {
		for _, a := range s.Addrs {
			if !s.Dead {
				servers = append(servers, net.JoinHostPort(a, "53"))
			}
		}
	}
	// У домена, которого нет, ближайшая зона - зона выше, вплоть до ru или com: её серверы словарём не спрашиваем.
	reg := inspect.Registrable(name)
	inside := zone.Name == reg || strings.HasSuffix(zone.Name, "."+reg)
	if err != nil || !zone.Direct || len(servers) == 0 || !inside {
		return dnsPath{}, false
	}
	p := dnsPath{opt: sweep.Options{Servers: servers, Control: dnsq.Control()}, how: "у серверов зоны", zone: zone.Name, direct: true}
	if iface := dnsq.Bound(); iface != "" {
		p.how += " мимо VPN через " + iface
	}
	return p, true
}

// resolvers: путь через резолверы - при перехвате порта 53 по DoH.
func resolvers() sweep.Options {
	if dnsq.Hijacked() {
		return sweep.Options{DoH: sweep.DoHServers}
	}
	return sweep.Options{Servers: dnsq.Resolvers, Recursive: true, Control: dnsq.Control()}
}

// archived: что локальный архив знает о домене; ready из parts - сколько частей выпуска уже стоит.
type archived struct {
	names        []string
	total        int
	release      string
	ready, parts int
	err          error
}

// lookupArchive: имена домена из локального архива - что видел обход интернета.
func lookupArchive(domain string) archived {
	base, err := namebase.Open(paths.Data("names"))
	if err != nil {
		return archived{err: err}
	}
	names, total, err := base.Lookup(domain, maxArchive)
	ready, parts := base.Parts()
	return archived{names, total, base.Release(), ready, parts, err}
}

// archive: имена из архива - в сбор; без архива - строка о том, как его поставить.
func (c *collector) archive(a archived) {
	st := c.state(fromBase)
	switch {
	case errors.Is(a.err, namebase.ErrNoBase):
		st.Note = "локального архива нет: tim data update --names"
		return
	case a.err != nil:
		st.Error = ui.Plain(ui.ShortErr(a.err))
		return
	}
	for _, n := range a.names {
		c.learn(n, fromBase)
	}
	st.Version, st.Note = a.release, "выпуск "+a.release
	if a.total > len(a.names) {
		st.Cut = true
		st.Note += ", взято " + strconv.Itoa(len(a.names)) + " из " + strconv.Itoa(a.total)
	}
	if a.ready < a.parts {
		st.Cut = true
		st.Note += ", стоит частей " + strconv.Itoa(a.ready) + " из " + strconv.Itoa(a.parts) + ": tim data update --names"
	}
}

// startWords запускает перебор словаря в фоне: он идёт, пока собирается первый круг и отвечают сторонние.
func (c *collector) startWords() {
	total, budget := wordsFull, sweepFull
	switch {
	case c.opt.Quick:
		total, budget = wordsQuick, sweepQuick
	case c.opt.night:
		budget = sweepNight
	case c.opt.Quiet:
		budget = sweepOwn
	}
	words := namebase.Words(total)
	names := make([]string, len(words))
	for i, w := range words {
		names[i] = w + "." + c.domain
	}
	c.swept = make(chan found, 1)
	c.wordsTotal = len(names)
	c.wordsLimit = budget + retryBudget + poolRounds*poolBudget + explicitBudget(budget) + retryBudget + wordsSlack
	go func() { c.swept <- c.alive(c.path(), names, budget) }()
}

// explicitBudget: сколько проверять имена под записью *, если основной проход шёл budget.
func explicitBudget(budget time.Duration) time.Duration {
	return min(budget, max(poolBudget, budget/2))
}

// takeWords забирает ответ перебора, если он готов или будет готов за wait; false - ещё идёт.
func (c *collector) takeWords(wait time.Duration) bool {
	if c.swept == nil {
		return true
	}
	select {
	case f := <-c.swept:
		c.swept = nil
		c.words(f)
		return true
	case <-time.After(wait):
		return false
	}
}

// words: имена, которые нашёл словарь, и строка о том, сколько он проверил и каким путём.
func (c *collector) words(f found) {
	st := c.state(fromWords)
	st.Checked, st.Total, st.Version, st.Note = f.checked, c.wordsTotal, namebase.WordsVersion, c.path().how
	st.Cut = f.cut
	if f.limited {
		st.Note += ", сервер ограничивает частоту"
	}
	if f.checked < c.wordsTotal && !c.path().direct && !c.opt.Quick {
		c.say("порт 53 перехвачен (VPN): словарь проверен не весь; весь - с --direct, но серверы зоны увидят твой адрес")
	}
	if f.starred > 0 {
		st.Note += ", под записью * " + strconv.Itoa(f.starred) + " слов, явно заведённых " + strconv.Itoa(len(f.firm))
	}
	names := c.take(f)
	if len(names) > maxHits {
		st.Cut = true
		st.Note += ", совпадений слишком много: взяты первые " + strconv.Itoa(maxHits)
		names = names[:maxHits]
	}
	for _, n := range names {
		c.learn(n, fromWords)
	}
}

// wordsLost: перебор не уложился в срок сбора - об этом строка, а не тишина.
func (c *collector) wordsLost() {
	if c.swept == nil {
		return
	}
	st := c.state(fromWords)
	st.Total, st.Version, st.Cut, st.Note = c.wordsTotal, namebase.WordsVersion, true, "не успел за срок сбора"
}

// take: имена, которые проверка признала существующими, надёжные первыми: заведённые явно под записью *, вершины
// дочерних зон, имена без адреса, живые. Имена без адреса и зоны запоминаются: под ними стоит искать глубже.
func (c *collector) take(f found) []string {
	for _, n := range f.firm {
		c.firmly[n] = true
	}
	for _, n := range f.nodes {
		if !slices.Contains(c.nodes, n) {
			c.nodes = append(c.nodes, n)
		}
	}
	for _, z := range f.zones {
		if !slices.Contains(c.zones, z) {
			c.zones = append(c.zones, z)
		}
	}
	return slices.Concat(f.firm, f.zones, f.nodes, f.live)
}

// alive: какие из имён есть в DNS, если спрашивать путём p. Имя, которое отвечает как запись *, считается, только
// если заведено явно; о ком судить нечем - не считается, а проход помечается неполным.
func (c *collector) alive(p dnsPath, names []string, budget time.Duration) found {
	if len(names) == 0 {
		return found{}
	}
	answers, limited := askNames(p, names, budget)
	out := found{limited: limited}
	var hits []sweep.Answer
	for i, a := range answers {
		a.Name = names[i]
		switch a.Kind {
		case sweep.Addr, sweep.CNAME, sweep.NoData:
			hits = append(hits, a)
			out.checked++
		case sweep.Refer:
			if !slices.Contains(out.zones, a.Zone) {
				out.zones = append(out.zones, a.Zone)
			}
			out.checked++
		case sweep.NX:
			out.checked++
		}
	}
	out.cut = out.checked < len(names)
	c.star(p, hits, min(poolBudget, budget))
	var blank []string
	for _, a := range hits {
		under, direct, known := c.poolOf(a.Name)
		switch covered := known && under.covers(a); {
		case !covered && !direct:
			out.cut = true
			fallthrough
		case covered:
			if a.Kind != sweep.NoData {
				blank = append(blank, a.Name)
			}
		case a.Kind == sweep.NoData:
			out.nodes = append(out.nodes, a.Name)
		default:
			out.live = append(out.live, a.Name)
		}
	}
	firm, all := explicit(p, blank, explicitBudget(budget))
	out.firm, out.starred, out.cut = firm, len(blank), out.cut || !all
	return out
}
