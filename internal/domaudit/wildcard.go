package domaudit

import (
	"slices"
	"strconv"
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

// blank: имя неотличимо от несуществующего - его адреса те же, что у wildcard. Имя из сертификата остаётся:
// сертификат выпускают на то, что есть.
func blank(source string, ips, wild []string) bool {
	if len(wild) == 0 || len(ips) == 0 || source == fromApex || source == fromCert {
		return false
	}
	return !slices.ContainsFunc(ips, func(ip string) bool { return !slices.Contains(wild, ip) })
}

// bruteforce: частые имена, которые резолвятся, с их адресами.
func bruteforce(domain string, wild []string) map[string][]string {
	out := map[string][]string{}
	for i, ips := range par.Map(Common, dnsWidth, func(p string) []string { return lookup(p + "." + domain) }) {
		if len(ips) > 0 && !blank(fromBrute, ips, wild) {
			out[Common[i]+"."+domain] = ips
		}
	}
	return out
}
