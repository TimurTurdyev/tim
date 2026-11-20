package harvest

import (
	"fmt"
	"net/netip"
	"strconv"
)

// ReverseZone: обратная зона сети /24 - "2.0.192.in-addr.arpa" для 192.0.2.0/24; для любой другой сети пусто.
func ReverseZone(net netip.Prefix) string {
	if !net.Addr().Is4() || net.Bits() != sweepBits {
		return ""
	}
	quad := net.Addr().As4()
	return fmt.Sprintf("%d.%d.%d.in-addr.arpa", quad[2], quad[1], quad[0])
}

// ReverseNames: имена PTR всех 256 адресов сети /24.
func ReverseNames(net netip.Prefix) []string {
	zone := ReverseZone(net)
	if zone == "" {
		return nil
	}
	out := make([]string, 256)
	for i := range out {
		out[i] = strconv.Itoa(i) + "." + zone
	}
	return out
}
