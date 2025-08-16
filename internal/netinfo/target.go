package netinfo

import (
	"errors"
	"net"
	"sort"
	"strconv"
	"strings"
)

type TargetKind int

const (
	KindHost TargetKind = iota
	KindNet
	KindDomain
	KindAS // все сети автономной системы, значение вида as:13238
)

// Target: что уводим в обход - адрес, подсеть или домен.
type Target struct {
	Kind  TargetKind
	Value string
}

// ParseTarget различает IPv4, подсеть CIDR и домен; подсеть приводится к адресу сети.
func ParseTarget(s string) (Target, error) {
	t := strings.TrimSpace(strings.ToLower(s))
	if i := strings.Index(t, "://"); i >= 0 {
		t, _, _ = strings.Cut(t[i+3:], "/")
	}
	t = strings.TrimSuffix(t, ".")
	if num, ok := asNumber(t); ok {
		return Target{KindAS, "as:" + num}, nil
	}
	if IsIPv4(t) {
		if net.ParseIP(t) == nil {
			return Target{}, errors.New("неверный адрес: " + s)
		}
		return Target{KindHost, t}, nil
	}
	if strings.Contains(t, "/") {
		ip, n, err := net.ParseCIDR(t)
		if err != nil || ip.To4() == nil {
			return Target{}, errors.New("неверная подсеть: " + s)
		}
		return Target{KindNet, n.String()}, nil
	}
	if h, _, err := net.SplitHostPort(t); err == nil {
		t = h
	}
	if !strings.Contains(t, ".") || strings.ContainsAny(t, " _") {
		return Target{}, errors.New("не адрес, не подсеть и не домен: " + s)
	}
	return Target{KindDomain, t}, nil
}

// PrivateTarget: адрес или подсеть целиком внутри частных диапазонов.
func PrivateTarget(t Target) bool {
	switch t.Kind {
	case KindHost:
		return IsPrivate(t.Value)
	case KindNet:
		ip, _, _ := net.ParseCIDR(t.Value)
		return ip != nil && IsPrivate(ip.String())
	}
	return false
}

// LookupIPv4 раскрывает домен во все A-записи, отсортированные.
func LookupIPv4(host string) []string {
	ips, err := net.LookupIP(host)
	if err != nil {
		return nil
	}
	var out []string
	seen := map[string]bool{}
	for _, ip := range ips {
		if v4 := ip.To4(); v4 != nil && !seen[v4.String()] {
			seen[v4.String()] = true
			out = append(out, v4.String())
		}
	}
	sort.Strings(out)
	return out
}

// TableRoute: строка таблицы netstat -rn -f inet; Dest - адрес или CIDR.
type TableRoute struct {
	Dest, Gateway, Flags, Iface string
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

func normalizeDest(s string) (string, bool) {
	addr, bits, hasBits := strings.Cut(s, "/")
	parts := strings.Split(addr, ".")
	if len(parts) == 0 || len(parts) > 4 {
		return "", false
	}
	for _, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 || n > 255 {
			return "", false
		}
	}
	if !hasBits {
		if len(parts) != 4 {
			return "", false
		}
		return addr, true
	}
	for len(parts) < 4 {
		parts = append(parts, "0")
	}
	return strings.Join(parts, ".") + "/" + bits, true
}

// Stale: статические маршруты через шлюз, который уже не шлюз физического интерфейса.
// only ограничивает проверку набором адресов и подсетей; nil - вся таблица.
func Stale(routes []TableRoute, gw string, only map[string]bool) []TableRoute {
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

// asNumber: as:13238, as13238 или AS13238.
func asNumber(s string) (string, bool) {
	rest, ok := strings.CutPrefix(s, "as")
	if !ok {
		return "", false
	}
	rest = strings.TrimPrefix(rest, ":")
	if n, err := strconv.ParseUint(rest, 10, 32); err != nil || n == 0 {
		return "", false
	}
	return rest, true
}
