package domaudit

import (
	"context"
	"errors"
	"net"
	"slices"
	"strconv"
	"time"

	"github.com/TimurTurdyev/tim/internal/dnsq"
	"github.com/TimurTurdyev/tim/internal/namebase"
	"github.com/TimurTurdyev/tim/internal/paths"
	"github.com/TimurTurdyev/tim/internal/sweep"
	"github.com/TimurTurdyev/tim/internal/ui"
)

const (
	wordsFull   = 100000 // весь встроенный словарь
	wordsQuick  = 20000
	sweepFull   = 20 * time.Second // столько же ждём сторонних: перебор идёт в это время
	sweepOwn    = 40 * time.Second // без сторонних ждать некого, зато словарь у серверов зоны проходит весь
	sweepQuick  = 3 * time.Second
	sweepNight  = time.Minute
	poolBudget  = 5 * time.Second
	maxHits     = 3000 // больше имён словарь не находит и у огромных зон: дальше похоже на незамеченную запись *
	maxExplicit = 2000
	maxArchive  = 10000 // больше имён в архиве только у площадок с чужими поддоменами
)

// dnsPath: каким путём идёт массовая проверка имён; how - словами, для строки источника.
type dnsPath struct {
	opt    sweep.Options
	how    string
	zone   string // вершина зоны, чьи серверы спрашиваем
	direct bool   // спрашиваем сами серверы зоны
}

// pool: что зона отвечает на несуществующее имя под родителем; пусто - честное "имени нет".
type pool struct {
	ips, targets []string
	nodata       bool
}

// found: что массовая проверка сказала об именах.
type found struct {
	live    []string // есть адрес или CNAME, и это не запись *
	blank   []string // неотличимы от записи * своего родителя
	nodes   []string // имя есть, адреса у него нет
	zones   []string // дочерние зоны, куда отсылают серверы
	checked int
	limited bool
}

// bind: запросы DNS с интерфейса мимо VPN, если так просили; не вышло - обычным путём и строкой в отчёте.
func (c *collector) bind() {
	if c.opt.Direct == "" || dnsq.Bound() == c.opt.Direct {
		return
	}
	if err := dnsq.Bind(c.opt.Direct); err != nil {
		c.say("мимо VPN через " + c.opt.Direct + " не вышло, запросы DNS идут обычным путём: " + ui.Plain(ui.ShortErr(err)))
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
	if err != nil || !zone.Direct || len(servers) == 0 {
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

// archived: что локальный архив знает о домене.
type archived struct {
	names   []string
	total   int
	release string
	err     error
}

// lookupArchive: имена домена из локального архива - что видел обход интернета.
func lookupArchive(domain string) archived {
	base, err := namebase.Open(paths.Data("names"))
	if err != nil {
		return archived{err: err}
	}
	names, total, err := base.Lookup(domain, maxArchive)
	return archived{names, total, base.Release(), err}
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
	go func() { c.swept <- c.alive(c.path(), names, budget) }()
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
	st.Cut = f.checked < c.wordsTotal
	if f.limited {
		st.Note += ", сервер ограничивает частоту"
	}
	if st.Cut && !c.path().direct && !c.opt.Quick {
		c.say("порт 53 перехвачен (VPN): словарь проверен не весь; весь - с --direct, но серверы зоны увидят твой адрес")
	}
	live := c.take(c.path(), f)
	if len(live) > maxHits {
		st.Note += ", совпадений слишком много: взяты первые " + strconv.Itoa(maxHits)
		live = live[:maxHits]
	}
	for _, n := range live {
		c.learn(n, fromWords)
	}
}

// take: имена, которые проверка признала существующими: живые, заведённые явно под записью *, имена без адреса и
// вершины дочерних зон. Последние два вида запоминаются: под ними стоит искать глубже.
func (c *collector) take(via dnsPath, f found) []string {
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
	return slices.Concat(f.live, c.explicit(via, f.blank), f.nodes, f.zones)
}

// alive: какие из имён есть в DNS, если спрашивать путём p. Имя под записью * считается, только если отвечает не
// так, как она.
func (c *collector) alive(p dnsPath, names []string, budget time.Duration) found {
	if len(names) == 0 {
		return found{}
	}
	opt := p.opt
	opt.Budget = budget
	res := sweep.Run(context.Background(), names, opt)
	out := found{checked: len(res.Answers), limited: res.Limited}
	var hits []sweep.Answer
	var parents []string
	for i, a := range res.Answers {
		a.Name = names[i]
		switch a.Kind {
		case sweep.Addr, sweep.CNAME:
			hits = append(hits, a)
			if p := parentOf(a.Name); !slices.Contains(parents, p) {
				parents = append(parents, p)
			}
		case sweep.NoData:
			out.nodes = append(out.nodes, a.Name)
		case sweep.Refer:
			if !slices.Contains(out.zones, a.Zone) {
				out.zones = append(out.zones, a.Zone)
			}
		}
	}
	pools := c.pools(p, parents)
	for _, a := range hits {
		if under := pools[parentOf(a.Name)]; under.covers(a) {
			out.blank = append(out.blank, a.Name)
		} else {
			out.live = append(out.live, a.Name)
		}
	}
	out.nodes = slices.DeleteFunc(out.nodes, func(n string) bool { return pools[parentOf(n)].nodata })
	return out
}

// pools: ответы зоны на несуществующие имена под каждым из родителей - у балансировщика каждый ответ свой.
func (c *collector) pools(via dnsPath, parents []string) map[string]pool {
	c.poolMu.Lock()
	defer c.poolMu.Unlock()
	var names []string
	nonce := "tim-nx-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	for _, p := range parents {
		if _, done := c.pool[p]; done {
			continue
		}
		c.pool[p] = pool{}
		for i := range wildTries {
			names = append(names, nonce+strconv.Itoa(i)+"."+p)
		}
	}
	if len(names) == 0 {
		return c.pool
	}
	opt := via.opt
	opt.Budget = poolBudget
	for i, a := range sweep.Run(context.Background(), names, opt).Answers {
		parent := parentOf(names[i])
		p := c.pool[parent]
		switch a.Kind {
		case sweep.Addr:
			p.ips = append(p.ips, a.IPs...)
		case sweep.CNAME:
			p.targets = append(p.targets, a.Target)
		case sweep.NoData:
			p.nodata = true
		}
		c.pool[parent] = p
	}
	return c.pool
}

// covers: ответ неотличим от ответа на несуществующее имя.
func (p pool) covers(a sweep.Answer) bool {
	if a.Kind == sweep.CNAME {
		return slices.Contains(p.targets, a.Target)
	}
	return len(p.ips) > 0 && !slices.ContainsFunc(a.IPs, func(ip string) bool { return !slices.Contains(p.ips, ip) })
}

// explicit: имена, неотличимые от записи *, но заведённые явно: под таким именем запись * уже не действует, и на
// несуществующее имя ниже зона отвечает "нет".
func (c *collector) explicit(via dnsPath, blank []string) []string {
	if len(blank) == 0 {
		return nil
	}
	blank = blank[:min(len(blank), maxExplicit)]
	probes := make([]string, len(blank))
	for i, n := range blank {
		probes[i] = "tim-nx." + n
	}
	opt := via.opt
	opt.Budget = poolBudget
	var out []string
	for i, a := range sweep.Run(context.Background(), probes, opt).Answers {
		if a.Kind == sweep.NX {
			c.firmly[blank[i]] = true
			out = append(out, blank[i])
		}
	}
	return out
}
