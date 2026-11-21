package domaudit

import (
	"cmp"
	"context"
	"slices"
	"strings"
	"time"

	"github.com/TimurTurdyev/tim/internal/dnsq"
	"github.com/TimurTurdyev/tim/internal/inspect"
	"github.com/TimurTurdyev/tim/internal/par"
	"github.com/TimurTurdyev/tim/internal/ui"
)

const recordsTimeout = 5 * time.Second

// records: почтовые серверы, серверы имён с адресами и TXT. Серверы имён есть у зоны, поэтому у поддомена - от
// регистрируемого домена.
func (c *collector) records() {
	ctx, cancel := context.WithTimeout(context.Background(), recordsTimeout)
	defer cancel()
	par.All(
		func() { c.rep.MX = mailServers(ctx, c.domain) },
		func() { c.rep.NS = nameServers(ctx, c.domain) },
		func() { c.rep.TXT = texts(ctx, c.domain) },
	)
	for _, s := range c.rep.MX {
		c.known[strings.ToLower(s.Name)] = s.IPs
		c.learn(s.Name, fromMX)
	}
	for _, s := range c.rep.NS {
		c.known[strings.ToLower(s.Name)] = s.IPs
		c.learn(s.Name, fromNS)
	}
	c.dnsNames()
}

func mailServers(ctx context.Context, domain string) []Server {
	records, _ := dnsq.Resolver().LookupMX(ctx, domain)
	var out []Server
	for _, r := range records {
		if host := strings.TrimSuffix(r.Host, "."); host != "" { // пустое имя - домен почту не принимает
			out = append(out, Server{Name: ui.Plain(host), Pref: int(r.Pref)})
		}
	}
	slices.SortStableFunc(out, func(a, b Server) int { return cmp.Or(cmp.Compare(a.Pref, b.Pref), strings.Compare(a.Name, b.Name)) })
	return withAddrs(out)
}

func nameServers(ctx context.Context, domain string) []Server {
	for _, zone := range []string{domain, inspect.Registrable(domain)} {
		records, _ := dnsq.Resolver().LookupNS(ctx, zone)
		if len(records) == 0 {
			continue
		}
		var out []Server
		for _, r := range records {
			out = append(out, Server{Name: ui.Plain(strings.TrimSuffix(r.Host, "."))})
		}
		slices.SortFunc(out, func(a, b Server) int { return strings.Compare(a.Name, b.Name) })
		return withAddrs(out)
	}
	return nil
}

func texts(ctx context.Context, domain string) []string {
	out, _ := dnsq.Resolver().LookupTXT(ctx, domain)
	for i := range out {
		out[i] = ui.Plain(out[i])
	}
	slices.Sort(out)
	return out
}

func withAddrs(list []Server) []Server {
	for i, ips := range par.Map(list, dnsWidth, func(s Server) []string { return lookup(s.Name) }) {
		list[i].IPs = ips
	}
	return list
}
