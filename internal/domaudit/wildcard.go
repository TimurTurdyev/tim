package domaudit

import (
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/TimurTurdyev/tim/internal/par"
)

// wildTries: сколько несуществующих имён спросить: за балансировщиком каждый ответ - один адрес из нескольких.
const wildTries = 8

// wildcard: адреса, которые DNS отдаёт на имя, которого нет; пусто - такого ответа нет.
func wildcard(domain string) []string {
	nonce := "tim-nx-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	tries := make([]string, wildTries)
	for i := range tries {
		tries[i] = nonce + strconv.Itoa(i) + "." + domain
	}
	out := slices.Concat(par.Map(tries, wildTries, lookup)...)
	slices.Sort(out)
	return slices.Compact(out)
}

// wildOf: адреса несуществующего имени под parent.
func (c *collector) wildOf(parent string) []string {
	if parent == c.domain {
		return c.wild
	}
	return c.wilds[parent]
}

// probeParents спрашивает несуществующее имя под каждым поддоменом, у которого появились угаданные дети: запись *
// бывает и ниже самого домена.
func (c *collector) probeParents(names map[string][]string, ips map[string][]string) {
	var fresh []string
	for n, labels := range names {
		p := parentOf(n)
		if _, done := c.wilds[p]; done || p == c.domain || n == c.domain || len(ips[n]) == 0 || firm(labels) || slices.Contains(fresh, p) {
			continue
		}
		fresh = append(fresh, p)
	}
	nonce := "tim-nx-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	for i, got := range par.Map(fresh, dnsWidth, func(p string) []string {
		return slices.Concat(lookup(nonce+"a."+p), lookup(nonce+"b."+p))
	}) {
		slices.Sort(got)
		c.wilds[fresh[i]] = slices.Compact(got)
	}
}

// parentOf: имя без левой метки.
func parentOf(name string) string {
	_, parent, _ := strings.Cut(name, ".")
	return parent
}

// blank: имя неотличимо от несуществующего - его адреса те же, что у wildcard, и ни один источник не называет
// его заведённым.
func blank(labels []string, ips, wild []string) bool {
	if len(wild) == 0 || len(ips) == 0 || firm(labels) {
		return false
	}
	return !slices.ContainsFunc(ips, func(ip string) bool { return !slices.Contains(wild, ip) })
}
