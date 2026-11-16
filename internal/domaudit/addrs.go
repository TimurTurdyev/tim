package domaudit

import (
	"context"
	"maps"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/TimurTurdyev/tim/internal/asn"
	"github.com/TimurTurdyev/tim/internal/dnsq"
	"github.com/TimurTurdyev/tim/internal/inspect"
	"github.com/TimurTurdyev/tim/internal/netinfo"
	"github.com/TimurTurdyev/tim/internal/par"
)

const (
	dnsTimeout = 3 * time.Second
	dnsWidth   = 96
	maxHops    = 8 // длиннее цепочек CNAME не бывает, разве что петля
)

// resolve: адреса имён и чьи это сети. Найденные перебором уже резолвились.
func (r *Report) resolve(names map[string]string, known map[string][]string) {
	all := slices.Sorted(maps.Keys(names))
	addrs := map[string][]string{}
	for i, ips := range par.Map(all, dnsWidth, func(name string) []string {
		if ips, ok := known[name]; ok {
			return ips
		}
		return lookup(name)
	}) {
		addrs[all[i]] = ips
	}
	found := len(all)
	all = slices.DeleteFunc(all, func(n string) bool { return blank(names[n], addrs[n], r.wild) })
	if dropped := found - len(all); dropped > 0 {
		r.Notes = append(r.Notes, "у домена запись *."+r.Domain+": отброшено имён, неотличимых от неё, - "+strconv.Itoa(dropped))
	}
	// Имя без адресов, но с CNAME, остаётся: запись ведёт в никуда, это надо видеть.
	targets := par.Map(all, dnsWidth, CNAME)
	cnames := map[string]string{}
	for i, t := range targets {
		cnames[all[i]] = strings.ToLower(t)
	}
	r.hops(cnames)
	byIP := map[string]*Addr{}
	for i, name := range all {
		ips := addrs[name]
		if len(ips) == 0 && targets[i] == "" {
			continue
		}
		h := Host{Name: name, IPs: ips, CNAME: targets[i], Source: names[name], Sources: r.by[name]}
		h.Internal = len(ips) > 0 && !slices.ContainsFunc(ips, netinfo.IsPublic)
		r.Hosts = append(r.Hosts, h)
		via := chain(cnames, h.Name)
		for _, ip := range ips {
			a := byIP[ip]
			if a == nil {
				a = &Addr{IP: ip, Internal: !netinfo.IsPublic(ip)}
				byIP[ip] = a
			}
			a.Hosts = append(a.Hosts, h.Name)
			a.cnames = append(a.cnames, via...)
			a.direct = a.direct || r.direct(via)
		}
	}
	ips := slices.Sorted(maps.Keys(byIP))
	owners := asn.Cymru(slices.DeleteFunc(slices.Clone(ips), func(ip string) bool { return byIP[ip].Internal }))
	for _, ip := range ips {
		a := byIP[ip]
		a.AS = owners[ip]
		r.Addrs = append(r.Addrs, *a)
	}
}

// hops дописывает CNAME имён домена, через которые идёт цепочка, но самих их в списке нет.
func (r *Report) hops(cnames map[string]string) {
	for range maxHops {
		var missing []string
		for _, t := range cnames {
			if _, known := cnames[t]; t != "" && !known && r.ours(t) {
				missing = append(missing, t)
			}
		}
		if len(missing) == 0 {
			return
		}
		for i, t := range par.Map(missing, dnsWidth, CNAME) {
			cnames[missing[i]] = strings.ToLower(t)
		}
	}
}

// direct: CNAME не уводит с домена на чужой сервис.
func (r *Report) direct(via []string) bool {
	return !slices.ContainsFunc(via, func(n string) bool { return !r.ours(n) })
}

// ours: имя той же регистрации, что и домен.
func (r *Report) ours(name string) bool {
	return inspect.Registrable(name) == inspect.Registrable(r.Domain)
}

// chain: по каким именам идёт CNAME; петля обрывается.
func chain(cnames map[string]string, name string) []string {
	var out []string
	for next := cnames[name]; next != "" && len(out) < maxHops && !slices.Contains(out, next); next = cnames[next] {
		out = append(out, next)
	}
	return out
}

// classify: обратные имена адресов, и по ним - узел CDN это или свой сервер.
func (r *Report) classify() {
	probes := par.NewGroup(dnsWidth)
	for i := range r.Addrs {
		a := &r.Addrs[i]
		if a.Internal {
			continue
		}
		probes.Go(func() {
			a.PTR = ptr(a.IP)
			a.CDN = asn.IsEdge(a.AS.Name, a.PTR, a.cnames...)
			a.Own = a.direct && !a.CDN
		})
	}
	probes.Wait()
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
