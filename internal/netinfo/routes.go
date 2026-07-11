package netinfo

import (
	"net/netip"
	"runtime"
	"strings"

	"github.com/TimurTurdyev/tim/internal/runner"
)

// TableRoute: строка таблицы маршрутов IPv4; у маршрута на один адрес префикс /32.
type TableRoute struct {
	Dest                  netip.Prefix
	Gateway, Flags, Iface string
}

// Target: куда маршрут, как это пишут в командах и конфиге.
func (r TableRoute) Target() string { return ShowDest(r.Dest) }

// ParseDest: адрес становится /32, у подсети обнуляются биты хоста.
func ParseDest(s string) (netip.Prefix, bool) {
	if a, err := netip.ParseAddr(s); err == nil && a.Is4() {
		return netip.PrefixFrom(a, 32), true
	}
	p, err := netip.ParsePrefix(s)
	if err != nil || !p.Addr().Is4() {
		return netip.Prefix{}, false
	}
	return p.Masked(), true
}

// ShowDest: адрес без /32, подсеть как есть.
func ShowDest(p netip.Prefix) string {
	if p.IsSingleIP() {
		return p.Addr().String()
	}
	return p.String()
}

// ParseRoutes разбирает таблицу; сокращённые сети вида 10.8/16 разворачиваются в 10.8.0.0/16.
func ParseRoutes(out string) []TableRoute {
	var list []TableRoute
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) < 4 {
			continue
		}
		dest, ok := normalizeDest(f[0])
		if !ok {
			continue
		}
		list = append(list, TableRoute{Dest: dest, Gateway: f[1], Flags: f[2], Iface: f[3]})
	}
	return list
}

// normalizeDest: netstat сокращает сети до 10.8/16, такие дополняются нулями.
func normalizeDest(s string) (netip.Prefix, bool) {
	addr, bits, hasBits := strings.Cut(s, "/")
	if !hasBits {
		return ParseDest(addr)
	}
	parts := strings.Split(addr, ".")
	if len(parts) > 4 {
		return netip.Prefix{}, false
	}
	for len(parts) < 4 {
		parts = append(parts, "0")
	}
	return ParseDest(strings.Join(parts, ".") + "/" + bits)
}

// ParseIPRoute переводит ip -4 route show в строки таблицы как у netstat: с флагами G (через шлюз),
// S (статический, не от ядра) и H (на один адрес).
func ParseIPRoute(out string) []TableRoute {
	var list []TableRoute
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) == 0 || f[0] == "default" || f[0] == "unreachable" || f[0] == "blackhole" {
			continue
		}
		dest, ok := normalizeDest(f[0])
		if !ok {
			continue
		}
		r := TableRoute{Dest: dest, Gateway: fieldAfter(f, "via"), Iface: fieldAfter(f, "dev"), Flags: "U"}
		if r.Gateway != "" {
			r.Flags += "G"
		}
		if fieldAfter(f, "proto") != "kernel" {
			r.Flags += "S"
		}
		if !strings.Contains(f[0], "/") {
			r.Flags += "H"
		}
		if r.Gateway == "" {
			r.Gateway = "link#" + r.Iface
		}
		list = append(list, r)
	}
	return list
}

// Stale: статические маршруты через шлюз, который уже не шлюз физического интерфейса.
// only ограничивает проверку набором адресов и подсетей; nil - вся таблица.
func Stale(routes []TableRoute, gw string, only map[netip.Prefix]bool) []TableRoute {
	var out []TableRoute
	for _, r := range routes {
		if !strings.Contains(r.Flags, "S") || !strings.Contains(r.Flags, "G") {
			continue
		}
		if !IsIPv4(r.Gateway) || r.Gateway == gw || IsTunnelIface(r.Iface) {
			continue
		}
		if only != nil && !only[r.Dest] {
			continue
		}
		out = append(out, r)
	}
	return out
}

// Table: вся таблица IPv4.
func Table() []TableRoute {
	if runtime.GOOS == "linux" {
		out, _ := runner.Output("ip", "-4", "route", "show")
		return ParseIPRoute(out)
	}
	out, _ := runner.Output("netstat", "-rn", "-f", "inet")
	return ParseRoutes(out)
}

// AddRouteCmd: команда, которая ставит маршрут до адреса или сети через шлюз. На Linux replace
// сразу заменяет старый, на macOS старый снимается отдельно (DelRouteCmd).
func AddRouteCmd(dest, gw, iface string) []string {
	if runtime.GOOS == "linux" {
		args := []string{"ip", "route", "replace", dest, "via", gw}
		if iface != "" {
			args = append(args, "dev", iface)
		}
		return args
	}
	return []string{"route", "-n", "add", kind(dest), dest, gw}
}

// DelRouteCmd: команда, которая снимает маршрут.
func DelRouteCmd(dest string) []string {
	if runtime.GOOS == "linux" {
		return []string{"ip", "route", "del", dest}
	}
	return []string{"route", "-n", "delete", kind(dest), dest}
}

func kind(dest string) string {
	if strings.Contains(dest, "/") {
		return "-net"
	}
	return "-host"
}
