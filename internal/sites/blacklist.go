package sites

import (
	"net"
	"slices"
	"strings"

	"github.com/TimurTurdyev/tim/internal/dnsbl"
	"github.com/TimurTurdyev/tim/internal/level"
	"github.com/TimurTurdyev/tim/internal/par"
)

// mxBlacklist: адреса почтовых серверов домена в чёрных списках. Письма с адреса из списка отклоняют,
// а ответы на них уходят в спам - это видно раньше, чем жалобы клиентов.
func mxBlacklist(mx []string) (level.Level, string) {
	var ips []string
	for _, m := range mx {
		host, _, _ := strings.Cut(m, " ")
		addrs, _ := net.LookupHost(host)
		for _, a := range addrs {
			if net.ParseIP(a).To4() != nil && !slices.Contains(ips, a) {
				ips = append(ips, a)
			}
		}
	}
	if len(ips) == 0 {
		return level.Skip, ""
	}
	results := par.Map(ips, 4, dnsbl.Check)
	var listed, unknown []string
	for i, rs := range results {
		for _, r := range rs {
			switch {
			case r.Listed:
				listed = append(listed, ips[i]+" в "+r.Zone)
			case r.Unknown != "" && !slices.Contains(unknown, r.Zone+" "+r.Unknown):
				unknown = append(unknown, r.Zone+" "+r.Unknown)
			}
		}
	}
	switch {
	case len(listed) > 0:
		return level.Bad, strings.Join(listed, ", ")
	case len(unknown) > 0:
		return level.OK, "не в списках; " + strings.Join(unknown, ", ")
	}
	return level.OK, "не в списках Spamhaus, Barracuda, SpamCop"
}
