package harvest

import (
	"net/netip"
	"regexp"
	"slices"
	"strings"
)

// Сеть из SPF обходится по /24; шире /22 - это тысячи запросов ради чужой почтовой сети.
const (
	sweepBits = 24
	wideBits  = 22
)

// SRV: службы, под которыми домены держат записи SRV; спрашивается "<служба>.<домен>".
var SRV = []string{
	"_sip._tcp", "_sips._tcp", "_xmpp-client._tcp", "_xmpp-server._tcp", "_autodiscover._tcp", "_caldav._tcp",
	"_carddav._tcp", "_imaps._tcp", "_submission._tcp", "_ldap._tcp", "_kerberos._tcp", "_matrix._tcp",
	"_minecraft._tcp", "_jabber._tcp", "_h323cs._tcp", "_stun._udp", "_turn._udp",
}

var (
	// Отчёты DMARC и TLS-RPT уходят на ящик (mailto:) или на страницу (https://), адреса идут через запятую.
	reportHostRe = regexp.MustCompile(`(?i)(?:mailto:[^@\s,;!]+@|https://(?:[^/@\s,;!]*@)?)([a-z0-9._-]+)`)
	iodefHostRe  = regexp.MustCompile(`(?i)^(?:mailto:[^@\s]+@|https?://(?:[^/@\s]*@)?)([a-z0-9._-]+)`)
)

// SPF: что названо в записи. Раскрывать ли Includes, решает вызывающий; Nets - сети /24 под обход обратных имён.
type SPF struct {
	Names    []string // из a, mx, exists, ptr
	Includes []string // из include и redirect
	IPs      []string // одиночные адреса ip4 и ip6
	Nets     []netip.Prefix
}

// addNet: одиночный адрес - в IPs; сеть ip4 не шире /22 - в Nets по /24; сети шире и сети ip6 не обходятся.
func (s *SPF) addNet(kind, value, mask string) {
	addr, err := netip.ParseAddr(value)
	if err != nil || addr.Zone() != "" || addr.Is4() != (kind == "ip4") {
		return
	}
	prefix := netip.PrefixFrom(addr, addr.BitLen())
	if mask != "" {
		if prefix, err = netip.ParsePrefix(value + "/" + mask); err != nil {
			return
		}
	}
	switch {
	case prefix.IsSingleIP():
		s.IPs = appendNew(s.IPs, addr.String())
	case addr.Is4() && prefix.Bits() >= wideBits:
		for _, part := range split24(prefix) {
			if !slices.Contains(s.Nets, part) {
				s.Nets = append(s.Nets, part)
			}
		}
	}
}

// ParseSPF разбирает запись TXT; ok=false - это не SPF.
func ParseSPF(txt string) (spf SPF, ok bool) {
	terms := strings.Fields(txt)
	if len(terms) == 0 || !strings.EqualFold(terms[0], "v=spf1") {
		return SPF{}, false
	}
	for _, term := range terms[1:] {
		kind, value, mask := splitTerm(term)
		switch kind {
		case "a", "mx", "exists", "ptr":
			spf.Names = appendNew(spf.Names, spfHost(value))
		case "include", "redirect":
			spf.Includes = appendNew(spf.Includes, spfHost(value))
		case "ip4", "ip6":
			spf.addNet(kind, value, mask)
		}
	}
	return spf, true
}

// MailHosts: куда запись DMARC или TLS-RPT шлёт отчёты - домены ящиков из mailto: и хосты из https://.
func MailHosts(txt string) []string {
	var out []string
	for _, match := range reportHostRe.FindAllStringSubmatch(txt, -1) {
		if host := hostName(match[1]); host != "" {
			out = append(out, host)
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// SOAMailHost: домен ящика из rname; первая метка - имя ящика, точка внутри неё записана как "\.".
func SOAMailHost(rname string) string {
	for i := 0; i < len(rname); i++ {
		switch rname[i] {
		case '\\':
			i++
		case '.':
			return hostName(rname[i+1:])
		}
	}
	return ""
}

// IodefHost: домен из значения записи CAA iodef - ящика после mailto: или страницы по http и https.
func IodefHost(value string) string {
	match := iodefHostRe.FindStringSubmatch(strings.Trim(strings.TrimSpace(value), `"`))
	if match == nil {
		return ""
	}
	return hostName(match[1])
}

// splitTerm: механизм без квалификатора, его значение и маска: "+a:mail.example.com/28" -> a, mail.example.com, 28.
func splitTerm(term string) (kind, value, mask string) {
	term, mask, _ = strings.Cut(strings.TrimLeft(term, "+-~?"), "/")
	kind = term
	if i := strings.IndexAny(term, ":="); i >= 0 {
		kind, value = term[:i], term[i+1:]
	}
	return strings.ToLower(kind), value, mask
}

// spfHost: имя из значения механизма; у макроса "%{i}._spf.example.com" известно только то, что правее его метки.
func spfHost(value string) string {
	if i := strings.LastIndexByte(value, '}'); i >= 0 {
		_, value, _ = strings.Cut(value[i+1:], ".")
	}
	return hostName(value)
}

// split24: сети /24, из которых состоит prefix, или та одна, в которую он входит.
func split24(prefix netip.Prefix) []netip.Prefix {
	quad := prefix.Masked().Addr().As4()
	quad[3] = 0
	var out []netip.Prefix
	for range 1 << max(0, sweepBits-prefix.Bits()) {
		out = append(out, netip.PrefixFrom(netip.AddrFrom4(quad), sweepBits))
		quad[2]++
	}
	return out
}

func appendNew(list []string, item string) []string {
	if item == "" || slices.Contains(list, item) {
		return list
	}
	return append(list, item)
}
