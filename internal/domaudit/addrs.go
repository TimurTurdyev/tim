package domaudit

import (
	"context"
	"maps"
	"slices"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/TimurTurdyev/tim/internal/asn"
	"github.com/TimurTurdyev/tim/internal/dnsq"
	"github.com/TimurTurdyev/tim/internal/inspect"
	"github.com/TimurTurdyev/tim/internal/internetdb"
	"github.com/TimurTurdyev/tim/internal/netinfo"
	"github.com/TimurTurdyev/tim/internal/par"
	"github.com/TimurTurdyev/tim/internal/ui"
)

const (
	dnsTimeout = 3 * time.Second
	dnsWidth   = 96
	maxHops    = 8 // длиннее цепочек CNAME не бывает, разве что петля
	maxScans   = 100
	nightScans = 20
	scanWidth  = 4
)

// resolve: новые имена, у которых есть адрес или CNAME, и их адреса.
func (c *collector) resolve(names map[string][]string) (hosts []*Host, touched map[string]*Addr) {
	all := slices.Sorted(maps.Keys(names))
	ips := map[string][]string{}
	for i, list := range par.Map(all, dnsWidth, func(name string) []string {
		if list, ok := c.known[name]; ok {
			return list
		}
		return lookup(name)
	}) {
		ips[all[i]] = list
	}
	c.probeParents(names, ips)
	for n, labels := range names {
		c.tried[n] = firm(labels)
		delete(c.dropped, n)
		if !c.firmly[n] && blank(labels, ips[n], c.wildOf(parentOf(n))) {
			c.dropped[n] = true
		}
	}
	all = slices.DeleteFunc(all, func(n string) bool { return c.dropped[n] })
	// Имя без адресов, но с CNAME, остаётся: запись ведёт в никуда, это надо видеть.
	targets := par.Map(all, dnsWidth, CNAME)
	for i, t := range targets {
		c.cnames[all[i]] = strings.ToLower(t)
	}
	c.hops()
	touched = map[string]*Addr{}
	for i, name := range all {
		if len(ips[name]) == 0 && targets[i] == "" {
			continue
		}
		h := &Host{Name: name, IPs: ips[name], CNAME: targets[i], Source: best(names[name])}
		h.Internal = len(h.IPs) > 0 && !slices.ContainsFunc(h.IPs, netinfo.IsPublic)
		c.hosts[name] = h
		hosts = append(hosts, h)
		via := chain(c.cnames, name)
		for _, ip := range h.IPs {
			a := c.addr(ip)
			a.Hosts = append(a.Hosts, name)
			a.cnames = append(a.cnames, via...)
			a.direct = a.direct || c.direct(via)
			touched[ip] = a
		}
	}
	return hosts, touched
}

// servers: адреса почтовых серверов и серверов имён; свой сервер - с именем той же регистрации.
func (c *collector) servers() map[string]*Addr {
	touched := map[string]*Addr{}
	link := func(s Server, role func(*Addr) *[]string) {
		for _, ip := range s.IPs {
			a := c.addr(ip)
			a.direct = a.direct || c.ours(s.Name)
			*role(a) = append(*role(a), s.Name)
			touched[ip] = a
		}
	}
	for _, s := range c.rep.MX {
		link(s, func(a *Addr) *[]string { return &a.MX })
	}
	for _, s := range c.rep.NS {
		link(s, func(a *Addr) *[]string { return &a.NS })
	}
	return touched
}

func (c *collector) addr(ip string) *Addr {
	a := c.addrs[ip]
	if a == nil {
		a = &Addr{IP: ip, Hosts: []string{}, Internal: !netinfo.IsPublic(ip)}
		c.addrs[ip] = a
	}
	return a
}

// hops дописывает CNAME имён домена, через которые идёт цепочка, но самих их в списке нет.
func (c *collector) hops() {
	for range maxHops {
		var missing []string
		for _, t := range c.cnames {
			if _, known := c.cnames[t]; t != "" && !known && c.ours(t) {
				missing = append(missing, t)
			}
		}
		if len(missing) == 0 {
			return
		}
		for i, t := range par.Map(missing, dnsWidth, CNAME) {
			c.cnames[missing[i]] = strings.ToLower(t)
		}
	}
}

// direct: CNAME не уводит с домена на чужой сервис.
func (c *collector) direct(via []string) bool {
	return !slices.ContainsFunc(via, func(n string) bool { return !c.ours(n) })
}

// ours: имя той же регистрации, что и домен.
func (c *collector) ours(name string) bool {
	return inspect.Registrable(strings.ToLower(name)) == inspect.Registrable(c.domain)
}

// chain: по каким именам идёт CNAME; петля обрывается.
func chain(cnames map[string]string, name string) []string {
	var out []string
	for next := cnames[name]; next != "" && len(out) < maxHops && !slices.Contains(out, next); next = cnames[next] {
		out = append(out, next)
	}
	return out
}

// enrich: чьи адреса, их обратные имена, узел CDN это или свой сервер, и где он стоит.
func (c *collector) enrich(touched map[string]*Addr) {
	list := slices.SortedFunc(maps.Values(touched), func(a, b *Addr) int { return strings.Compare(a.IP, b.IP) })
	var ask []string
	for _, a := range list {
		if !a.Internal && !a.named {
			ask = append(ask, a.IP)
		}
	}
	owners := asn.Cymru(ask)
	group := par.NewGroup(dnsWidth)
	for _, a := range list {
		if a.Internal {
			continue
		}
		group.Go(func() {
			if !a.named {
				a.named, a.AS, a.PTR = true, owners[a.IP], ptr(a.IP)
			}
			a.CDN = asn.IsEdge(a.AS.Name, a.PTR, a.cnames...)
			a.Own = a.direct && !a.CDN
		})
	}
	group.Wait()
	var standing, own []*Addr
	for _, a := range list {
		c.learn(a.PTR, fromPTR)
		if where, ok := asn.Where(a.IP); ok && !a.CDN && !a.Internal {
			a.Country = where
		}
	}
	if c.opt.Quiet {
		return
	}
	for _, a := range list {
		if !a.CDN && !a.Internal && a.Country == "" { // узел CDN отвечает отовсюду, места у него нет
			standing = append(standing, a)
		}
		if a.Own && !a.seen && netinfo.IsIPv4(a.IP) {
			own = append(own, a)
		}
	}
	if c.opt.night {
		standing = nil
	}
	par.All(func() { places(standing) }, func() { c.services(own) })
	for _, a := range list {
		if a.CDN {
			a.Country = ""
		}
	}
}

// services: что сканеры видели на своих адресах; самих адресов не трогаем.
func (c *collector) services(list []*Addr) {
	for _, a := range list {
		a.seen = true
	}
	limit := maxScans
	if c.opt.night {
		limit = nightScans
	}
	room := max(0, limit-c.scanned)
	c.scanned += len(list)
	list = list[:min(len(list), room)]
	type answer struct {
		info internetdb.Info
		err  error
	}
	var failed atomic.Bool // не ответил про один адрес - не ответит и про остальные
	for i, got := range par.Map(list, scanWidth, func(a *Addr) answer {
		if failed.Load() {
			return answer{}
		}
		info, err := internetdb.Look(a.IP)
		if err != nil {
			failed.Store(true)
		}
		return answer{info, err}
	}) {
		if got.err != nil {
			c.rep.portsFailed = true
			c.fail("порты, InternetDB: " + ui.Plain(netError(got.err).Error()))
			continue
		}
		list[i].Scanned = !c.rep.portsFailed
		for _, n := range got.info.Names {
			c.learn(n, fromScan)
		}
		switch a := list[i]; {
		case got.info.CDN():
			a.CDN, a.Own = true, false
		case !got.info.Empty():
			a.Services = &got.info
		}
	}
}

// places: где стоят серверы - одним запросом на сеть.
func places(list []*Addr) {
	var nets []string
	for _, a := range list {
		if a.AS.Prefix != "" && a.Country == "" && !slices.Contains(nets, a.AS.Prefix) {
			nets = append(nets, a.AS.Prefix)
		}
	}
	found := par.Map(nets, par.Limit, func(net string) []asn.Place {
		list, _ := asn.Places(net)
		return list
	})
	for _, a := range list {
		i := slices.Index(nets, a.AS.Prefix)
		if i < 0 {
			continue
		}
		if country := asn.PlaceOf(found[i], a.IP).Country; country != asn.Nowhere {
			a.Country = country
		}
	}
}

func lookup(name string) []string {
	ctx, cancel := context.WithTimeout(context.Background(), dnsTimeout)
	defer cancel()
	addrs, err := dnsq.Resolver().LookupIPAddr(ctx, name)
	if err != nil {
		return nil
	}
	var out []string
	for _, a := range addrs {
		out = append(out, a.IP.String())
	}
	sort.Strings(out)
	return out
}

func ptr(ip string) string {
	ctx, cancel := context.WithTimeout(context.Background(), dnsTimeout)
	defer cancel()
	names, err := dnsq.Resolver().LookupAddr(ctx, ip)
	if err != nil || len(names) == 0 {
		return ""
	}
	return strings.TrimSuffix(names[0], ".")
}
