package netinfo

import "net/netip"

// notPublic: адреса, за которыми в интернете никого нет: частные, служебные, для документации и проверок.
var notPublic = parsePrefixes(
	"0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8", "169.254.0.0/16", "172.16.0.0/12", "192.0.0.0/24",
	"192.0.2.0/24", "192.168.0.0/16", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "224.0.0.0/3",
	"::/127", "100::/64", "2001:db8::/32", "fc00::/7", "fe80::/10", "ff00::/8",
)

// IsPublic: адрес живёт в интернете. Остальные наружу не отдаём: сервис в сети про них не знает, а видно устройство сети.
func IsPublic(ip string) bool {
	a, err := netip.ParseAddr(ip)
	if err != nil {
		return false
	}
	a = a.Unmap().WithZone("")
	for _, p := range notPublic {
		if p.Contains(a) {
			return false
		}
	}
	return true
}

func parsePrefixes(list ...string) []netip.Prefix {
	out := make([]netip.Prefix, len(list))
	for i, s := range list {
		out[i] = netip.MustParsePrefix(s)
	}
	return out
}
