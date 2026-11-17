package domaudit

import (
	"cmp"
	"slices"
	"strings"
)

// Network: сеть владельца и что в ней у домена. Узлы CDN и серверы одного владельца - разные строки.
type Network struct {
	ASN       string   `json:"asn,omitempty"`
	Owner     string   `json:"owner,omitempty"` // пусто - неизвестно
	Prefixes  []string `json:"prefixes,omitempty"`
	Countries []string `json:"countries,omitempty"`
	CDN       bool     `json:"cdn,omitempty"`
	Internal  bool     `json:"internal,omitempty"`
	MX        bool     `json:"mx,omitempty"`
	NS        bool     `json:"ns,omitempty"`
	Addrs     int      `json:"addrs"`
	Hosts     int      `json:"hosts"`
}

// Country: сколько адресов и имён домена стоит в стране; CDN - отвечает отовсюду, страны нет.
type Country struct {
	Code  string `json:"code,omitempty"` // пусто - неизвестно
	CDN   bool   `json:"cdn,omitempty"`
	Addrs int    `json:"addrs"`
	Hosts int    `json:"hosts"`
}

// Networks: свод по сетям - где больше имён, выше.
func Networks(rep *Report) []Network {
	type key struct {
		asn           string
		cdn, internal bool
	}
	found := map[key]*Network{}
	hosts := map[key]map[string]bool{}
	var order []key
	for _, a := range rep.Addrs {
		k := key{a.AS.ASN, a.CDN, a.Internal}
		n := found[k]
		if n == nil {
			n = &Network{ASN: a.AS.ASN, Owner: a.AS.Owner(), CDN: a.CDN, Internal: a.Internal}
			found[k], hosts[k] = n, map[string]bool{}
			order = append(order, k)
		}
		n.Addrs++
		n.MX, n.NS = n.MX || len(a.MX) > 0, n.NS || len(a.NS) > 0
		n.Prefixes = addOnce(n.Prefixes, a.AS.Prefix)
		n.Countries = addOnce(n.Countries, a.Country)
		for _, h := range a.Hosts {
			hosts[k][h] = true
		}
	}
	out := make([]Network, 0, len(order))
	for _, k := range order {
		n := found[k]
		n.Hosts = len(hosts[k])
		slices.Sort(n.Countries)
		out = append(out, *n)
	}
	slices.SortStableFunc(out, func(a, b Network) int {
		return cmp.Or(cmp.Compare(b.Hosts, a.Hosts), cmp.Compare(b.Addrs, a.Addrs), strings.Compare(a.Owner, b.Owner))
	})
	return out
}

// Countries: свод по странам - где больше имён, выше; внутренние адреса не в счёт.
func Countries(rep *Report) []Country {
	type key struct {
		code string
		cdn  bool
	}
	found := map[key]*Country{}
	hosts := map[key]map[string]bool{}
	for _, a := range rep.Addrs {
		if a.Internal {
			continue
		}
		k := key{a.Country, a.CDN}
		if found[k] == nil {
			found[k], hosts[k] = &Country{Code: a.Country, CDN: a.CDN}, map[string]bool{}
		}
		found[k].Addrs++
		for _, h := range a.Hosts {
			hosts[k][h] = true
		}
	}
	var out []Country
	for k, c := range found {
		c.Hosts = len(hosts[k])
		out = append(out, *c)
	}
	slices.SortFunc(out, func(a, b Country) int {
		return cmp.Or(cmp.Compare(b.Hosts, a.Hosts), cmp.Compare(b.Addrs, a.Addrs), strings.Compare(a.Code, b.Code))
	})
	return out
}

func addOnce(list []string, s string) []string {
	if s == "" || slices.Contains(list, s) {
		return list
	}
	return append(list, s)
}
