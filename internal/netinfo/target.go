package netinfo

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/idna"
)

// lastGood: последний удачный ответ по домену. Пока VPN переключается, резолвер может молчать,
// а страж маршрутов в это время работать должен.
var lastGood sync.Map

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

// ParseTarget различает IPv4, подсеть CIDR, номер AS и домен; подсеть приводится к адресу сети,
// /32 - к адресу, у адреса и домена отбрасываются схема, порт и путь.
func ParseTarget(s string) (Target, error) {
	t := strings.TrimSpace(strings.ToLower(s))
	if i := strings.Index(t, "://"); i >= 0 {
		t, _, _ = strings.Cut(t[i+3:], "/")
	}
	t = strings.TrimSuffix(t, ".")
	if num, ok := asNumber(t); ok {
		return Target{KindAS, "as:" + num}, nil
	}
	if h, _, err := net.SplitHostPort(t); err == nil {
		t = h
	}
	if first, _, cut := strings.Cut(t, "/"); cut && !ipv4Shape.MatchString(first) {
		t = first
	}
	switch {
	case strings.Contains(t, "/"):
		p, err := netip.ParsePrefix(t)
		if err != nil || !p.Addr().Is4() {
			return Target{}, errors.New("неверная подсеть: " + s)
		}
		if p = p.Masked(); p.IsSingleIP() {
			return Target{KindHost, p.Addr().String()}, nil
		}
		return Target{KindNet, p.String()}, nil
	case ipv4Shape.MatchString(t):
		a, err := netip.ParseAddr(t)
		if err != nil {
			return Target{}, errors.New("неверный адрес: " + s)
		}
		return Target{KindHost, a.String()}, nil
	}
	domain, err := idna.Lookup.ToASCII(t)
	if err != nil || !strings.Contains(domain, ".") || strings.ContainsAny(domain, " _") {
		return Target{}, errors.New("не адрес, не подсеть и не домен: " + s)
	}
	return Target{KindDomain, domain}, nil
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

// LookupIPv4 раскрывает домен во все A-записи, отсортированные; ждёт не дольше 3 секунд.
func LookupIPv4(host string) []string {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	ips, err := net.DefaultResolver.LookupIP(ctx, "ip4", host)
	if err != nil {
		if old, ok := lastGood.Load(host); ok {
			return old.([]string)
		}
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
	lastGood.Store(host, out)
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
	n, err := strconv.ParseUint(strings.TrimPrefix(rest, ":"), 10, 32)
	if err != nil || n == 0 {
		return "", false
	}
	return strconv.FormatUint(n, 10), true
}
