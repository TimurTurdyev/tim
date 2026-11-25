package domaudit

import (
	"cmp"
	"context"
	"crypto/tls"
	"crypto/x509"
	"maps"
	"net"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/dns/dnsmessage"

	"github.com/TimurTurdyev/tim/internal/dnsq"
	"github.com/TimurTurdyev/tim/internal/harvest"
	"github.com/TimurTurdyev/tim/internal/namebase"
	"github.com/TimurTurdyev/tim/internal/netinfo"
	"github.com/TimurTurdyev/tim/internal/par"
	"github.com/TimurTurdyev/tim/internal/sweep"
	"github.com/TimurTurdyev/tim/internal/webnet"
)

const (
	maxIncludes   = 10 // столько записей SPF раскрываем внутри домена
	maxNets       = 8  // сетей /24, чьи обратные имена обходим
	netCandidates = 16
	netBudget     = 8 * time.Second
	maxHandshakes = 20 // адресов, у которых берём сертификат без имени
	handshakeWait = 2 * time.Second
	handshakeWide = 16
	maxSeeds      = 300 // имён, по образцу которых ищем соседние
	maxPatterns   = 20000
	deepBudget    = 8 * time.Second
	deepParents   = 6    // поддоменов, под которыми пробуем словарь
	deepWords     = 2000 // и сколько слов под каждым
)

// mailPorts: где почтовый сервер отдаёт сертификат сразу, без STARTTLS.
var mailPorts = []string{"465", "993"}

// dnsNames: имена из записей самого домена - SPF, SRV и SOA; адреса и сети почты из SPF.
func (c *collector) dnsNames() {
	c.spf(c.rep.TXT, 0)
	for _, targets := range par.Map(harvest.SRV, len(harvest.SRV), func(service string) []string {
		ctx, cancel := context.WithTimeout(context.Background(), dnsTimeout)
		defer cancel()
		_, list, _ := dnsq.Resolver().LookupSRV(ctx, "", "", service+"."+c.domain)
		var out []string
		for _, s := range list {
			out = append(out, strings.TrimSuffix(s.Target, "."))
		}
		return out
	}) {
		c.learnAll(targets, fromSRV)
	}
	if mname, mbox, ok := zoneOwner(c.domain); ok {
		c.learnAll([]string{mname, mbox}, fromNS)
	}
	for _, name := range par.Map(c.mailIPs, dnsWidth, ptr) {
		c.learn(name, fromPTR)
	}
}

// spf: имена, адреса и сети из записей SPF; ссылки на записи внутри домена раскрываются.
func (c *collector) spf(texts []string, depth int) {
	for _, txt := range texts {
		rec, ok := harvest.ParseSPF(txt)
		if !ok {
			continue
		}
		c.learnAll(rec.Names, fromSPF)
		c.mailIPs = append(c.mailIPs, rec.IPs...)
		c.mailNets = append(c.mailNets, rec.Nets...)
		for _, inc := range rec.Includes {
			if !c.ours(inc) || depth >= maxIncludes || slices.Contains(c.included, inc) {
				continue
			}
			c.included = append(c.included, inc)
			c.learn(inc, fromSPF)
			ctx, cancel := context.WithTimeout(context.Background(), dnsTimeout)
			more, _ := dnsq.Resolver().LookupTXT(ctx, inc)
			cancel()
			c.spf(more, depth+1)
		}
	}
}

// learnAll: имена домена - с подписью; чужие домены из сертификатов - в связанные. Чужое из SPF, SRV, SOA и с
// соседних сайтов своего сервера связанным не считается: отчёт называет связанные "в тех же сертификатах".
func (c *collector) learnAll(names []string, from string) {
	subs, related := splitNames(names, c.domain)
	for _, n := range subs {
		c.learn(n, from)
	}
	if from == fromHostCert || from == fromSiteCert {
		c.related = append(c.related, related...)
	}
}

// zoneOwner: сервер и домен ящика из SOA зоны; ok=false - зона не отвечает.
func zoneOwner(zone string) (mname, mbox string, ok bool) {
	msg, err := dnsq.Exchange(dnsq.Resolvers[0], zone, dnsmessage.TypeSOA, true)
	if err != nil {
		return "", "", false
	}
	var p dnsmessage.Parser
	h, err := p.Start(msg)
	if err != nil || h.RCode == dnsmessage.RCodeServerFailure || h.RCode == dnsmessage.RCodeRefused {
		return "", "", false
	}
	_ = p.SkipAllQuestions()
	answers, _ := p.AllAnswers()
	authority, _ := p.AllAuthorities()
	for _, r := range slices.Concat(answers, authority) {
		if soa, isSOA := r.Body.(*dnsmessage.SOAResource); isSOA {
			return strings.TrimSuffix(strings.ToLower(soa.NS.String()), "."), harvest.SOAMailHost(soa.MBox.String()), true
		}
	}
	return "", "", true
}

// inside: сайты, которые настроены на своих серверах, - из их сохранённого обхода.
func (c *collector) inside() {
	if c.opt.Inside == nil || c.asked {
		return
	}
	c.asked = true
	var ips []string
	for _, a := range c.addrs {
		if a.direct && !a.CDN && !a.Internal {
			ips = append(ips, a.IP)
		}
	}
	slices.Sort(ips)
	var servers []string
	for _, in := range c.opt.Inside(ips) {
		c.learnAll(in.Names, fromInside)
		servers = append(servers, in.Server+" (обход от "+in.At.Local().Format("02.01")+")")
	}
	if len(servers) > 0 {
		c.state(fromInside).Note = strings.Join(servers, ", ")
	}
}

// reverse: обратные имена сетей домена - названных в SPF и тех, где стоят его серверы.
func (c *collector) reverse() {
	nets := c.ownNets()
	if len(nets) == 0 {
		return
	}
	var names []string
	for _, n := range nets {
		names = append(names, harvest.ReverseNames(n)...)
	}
	opt := resolvers()
	opt.Type, opt.Budget = dnsmessage.TypePTR, netBudget
	c.note("обратные имена сетей: " + strconv.Itoa(len(nets)))
	res := sweep.Run(context.Background(), names, opt)
	for _, a := range res.Answers {
		if a.Target != "" {
			c.learn(a.Target, fromPTR)
		}
	}
	c.netsDone += len(nets)
	for _, n := range nets {
		c.ranOver(fromPTR, []string{n.String()})
	}
	st := c.state(fromPTR)
	st.Note = "обойдено сетей /24: " + strconv.Itoa(c.netsDone)
	if left := len(names) - len(res.Answers) + res.Unanswered; left > 0 {
		st.Cut = true
		st.Note += ", без ответа адресов: " + strconv.Itoa(left)
	}
}

// ownNets: сети /24, которые ещё не обходили и которые похожи на свои: названа в SPF, обратную зону держит
// сам домен или в сети два его адреса и у одного обратное имя в домене. Зона, которая не отвечает, не обходится.
func (c *collector) ownNets() []netip.Prefix {
	type fact struct {
		addrs  int
		named  bool // у адреса домена обратное имя в домене
		listed bool // сеть названа в SPF
	}
	facts := map[netip.Prefix]*fact{}
	at := func(p netip.Prefix) *fact {
		if facts[p] == nil {
			facts[p] = &fact{}
		}
		return facts[p]
	}
	for _, n := range c.mailNets {
		for _, p := range split24(n) {
			at(p).listed = true
		}
	}
	for _, a := range c.addrs {
		ip, err := netip.ParseAddr(a.IP)
		if err != nil || !ip.Is4() || a.Internal || a.CDN || !a.direct {
			continue
		}
		f := at(netip.PrefixFrom(ip, 24).Masked())
		f.addrs++
		f.named = f.named || a.PTR != "" && c.ours(a.PTR)
	}
	var list []netip.Prefix
	for p := range facts {
		if !c.nets[p] {
			list = append(list, p)
		}
	}
	slices.SortFunc(list, func(a, b netip.Prefix) int { return a.Addr().Compare(b.Addr()) })
	list = list[:min(len(list), netCandidates)]
	type verdict struct{ alive, ours bool }
	checked := par.Map(list, len(list), func(p netip.Prefix) verdict {
		mname, mbox, ok := zoneOwner(harvest.ReverseZone(p))
		return verdict{ok, ok && (c.ours(mname) || c.ours(mbox))}
	})
	var out []netip.Prefix
	for i, p := range list {
		c.nets[p] = true
		f := facts[p]
		if checked[i].alive && (f.listed || checked[i].ours || f.addrs >= 2 && f.named) && len(out)+c.netsDone < maxNets {
			out = append(out, p)
		}
	}
	return out
}

// split24: сеть /24../22 по сетям /24.
func split24(n netip.Prefix) []netip.Prefix {
	if !n.Addr().Is4() || n.Bits() > 24 || n.Bits() < 22 {
		return nil
	}
	var out []netip.Prefix
	p := netip.PrefixFrom(n.Masked().Addr(), 24)
	for range 1 << (24 - n.Bits()) {
		out = append(out, p)
		last := p.Addr().As4()
		last[2]++
		p = netip.PrefixFrom(netip.AddrFrom4(last), 24)
	}
	return out
}

// handshakes: сертификаты, которые свои серверы отдают без имени: сайт по умолчанию на 443, у почтовых - почта.
func (c *collector) handshakes() {
	if c.opt.Quick || c.opt.night {
		return
	}
	var targets []string
	for _, a := range c.addrs {
		if !a.Own || a.shaken || !netinfo.IsIPv4(a.IP) {
			continue
		}
		a.shaken = true
		targets = append(targets, net.JoinHostPort(a.IP, "443"))
		if len(a.MX) > 0 {
			for _, port := range mailPorts {
				targets = append(targets, net.JoinHostPort(a.IP, port))
			}
		}
	}
	slices.Sort(targets)
	targets = targets[:min(len(targets), maxHandshakes)]
	for _, cert := range par.Map(targets, handshakeWide, defaultCert) {
		if cert == nil {
			continue
		}
		_, names := certNames(cert)
		c.learnAll(names, fromHostCert)
		c.certs = append(c.certs, cert)
	}
}

// defaultCert: сертификат, который сервер отдаёт, когда имя не названо; nil - не ответил.
func defaultCert(address string) *x509.Certificate {
	conn, err := webnet.Dial("tcp", address, handshakeWait)
	if err != nil {
		return nil
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(handshakeWait))
	client := tls.Client(conn, &tls.Config{InsecureSkipVerify: true}) //nolint:gosec // нужен сам сертификат, а не доверие к нему
	if client.Handshake() != nil {
		return nil
	}
	if certs := client.ConnectionState().PeerCertificates; len(certs) > 0 {
		return certs[0]
	}
	return nil
}

// deepen: третий круг - имена со страниц и сертификатов второго, соседи в журналах сертификатов, соседи
// найденных по образцу, словарь под поддоменами, у которых есть свои дети.
func (c *collector) deepen() {
	if c.opt.Quick {
		return
	}
	c.probing.Wait()
	c.group.Wait()
	c.gather()
	c.near()
	c.note("имена по образцу найденных")
	c.guess(fromPattern, c.alive(c.path(), c.patterns(), deepBudget))
	c.under()
	c.round(nil)
}

// guess: находки проверки догадок - имён по образцу или словаря под поддоменами - со строкой источника.
func (c *collector) guess(label string, f found) {
	for _, n := range c.take(f) {
		c.learn(n, label)
	}
	if f.cut {
		st := c.state(label)
		st.Cut, st.Note = true, "проверено не всё"
	}
}

// gather: имена, которые назвали сами сайты, - из сертификатов и страниц.
func (c *collector) gather() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, h := range c.hosts {
		for _, n := range h.certNames {
			c.learn(n, fromSiteCert)
		}
		for _, n := range h.pageNames {
			c.learn(n, fromPage)
		}
		if h.cert != nil && !slices.Contains(c.certs, h.cert) {
			c.certs = append(c.certs, h.cert)
		}
	}
}

// patterns: кандидаты по образцу живых имён - соседние номера и окружения; надёжные имена первыми.
func (c *collector) patterns() []string {
	var seeds []*Host
	for _, h := range c.hosts {
		if h.Name != c.domain && (len(h.IPs) > 0 || h.CNAME != "") {
			seeds = append(seeds, h)
		}
	}
	slices.SortFunc(seeds, func(a, b *Host) int {
		if d := rankOf(a.Source) - rankOf(b.Source); d != 0 {
			return d
		}
		return strings.Compare(a.Name, b.Name)
	})
	seen := map[string]bool{}
	var out []string
	for _, h := range seeds[:min(len(seeds), maxSeeds)] {
		c.ranOver(fromPattern, []string{h.Name})
		for _, n := range harvest.Mutate(h.Name, c.domain) {
			if _, tried := c.tried[n]; tried || seen[n] || len(out) >= maxPatterns {
				continue
			}
			seen[n] = true
			out = append(out, n)
		}
	}
	return out
}

// under: словарь под поддоменами, у которых есть дети: дочерние зоны, имена без адреса и родители найденных имён.
func (c *collector) under() {
	kids := map[string]int{}
	for name := range c.hosts {
		if p := parentOf(name); name != c.domain && p != c.domain && strings.HasSuffix(p, "."+c.domain) {
			kids[p]++
		}
	}
	busy := slices.SortedFunc(maps.Keys(kids), func(a, b string) int {
		return cmp.Or(cmp.Compare(kids[b], kids[a]), strings.Compare(a, b))
	})
	var parents []string
	for _, p := range slices.Concat(c.zones, c.nodes, busy) {
		// Под записью * словарь ответит на всё: такие родители уже видны по ответу на несуществующее имя.
		if len(c.wilds[p]) == 0 && !c.starred(p) && !slices.Contains(parents, p) && len(parents) < deepParents {
			parents = append(parents, p)
		}
	}
	if len(parents) == 0 {
		return
	}
	c.note("словарь под " + strings.Join(parents, ", "))
	c.ranOver(fromDeep, parents)
	words := namebase.Words(deepWords)
	// У дочерней зоны свои серверы: серверы родителя на её имена только отсылают.
	byPath := map[string][]string{}
	paths := map[string]dnsPath{"": c.path()}
	for _, parent := range parents {
		key := ""
		if c.path().direct {
			if p, ok := zonePath(parent); ok && p.zone != c.path().zone {
				key, paths[p.zone] = p.zone, p
			}
		}
		byPath[key] = append(byPath[key], parent)
	}
	for _, key := range slices.Sorted(maps.Keys(byPath)) {
		names := make([]string, 0, len(words)*len(byPath[key]))
		for _, w := range words {
			for _, parent := range byPath[key] {
				names = append(names, w+"."+parent)
			}
		}
		c.guess(fromDeep, c.alive(paths[key], names, deepBudget))
	}
}
