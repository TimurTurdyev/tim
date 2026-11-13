// Package netinfo: адреса, цели обхода, маршруты, интерфейсы, порты и VPN macOS и Linux через системные утилиты.
package netinfo

import (
	"context"
	"net"
	"net/netip"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// PrivateNets: частные сети IPv4 - 10/8, 172.16/12, 192.168/16.
var PrivateNets = []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"}

// ipv4Shape: похоже на IPv4 по виду, даже если числа неверные (300.1.1.1) - это ошибка, а не домен.
var ipv4Shape = regexp.MustCompile(`^\d{1,3}(\.\d{1,3}){3}$`)

// lastGood: последний удачный ответ по домену. Пока VPN переключается, резолвер может молчать,
// а страж маршрутов в это время работать должен.
var lastGood sync.Map

// IsIPv4: строка - правильный адрес IPv4.
func IsIPv4(s string) bool {
	a, err := netip.ParseAddr(s)
	return err == nil && a.Is4()
}

// IsPrivate: адрес из PrivateNets. Такие адреса достижимы только через
// VPN, и маршрут в обход им только навредит.
func IsPrivate(ip string) bool {
	a, err := netip.ParseAddr(ip)
	return err == nil && a.Is4() && a.IsPrivate()
}

// ResolveIPv4 через системный резолвер.
func ResolveIPv4(host string) string {
	if IsIPv4(host) {
		return host
	}
	if ips := LookupIPv4(host); len(ips) > 0 {
		return ips[0]
	}
	return ""
}

// HostPort: из "host", "host:port" или url достаёт хост и порт.
func HostPort(s, defPort string) (string, string) {
	if u, err := url.Parse(s); err == nil && u.Host != "" {
		s = u.Host
	}
	if h, p, err := net.SplitHostPort(s); err == nil {
		return h, p
	}
	return strings.TrimSuffix(s, "/"), defPort
}

// LookupIPv4 раскрывает домен во все A-записи, отсортированные; ждёт не дольше 3 секунд.
func LookupIPv4(host string) []string { return lookupIP(host, "ip4") }

// LookupIPv6: адреса IPv6 домена - обход закрывает их, чтобы программы шли к домену по IPv4.
func LookupIPv6(host string) []string { return lookupIP(host, "ip6") }

func lookupIP(host, network string) []string {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	key := network + " " + host
	ips, err := net.DefaultResolver.LookupIP(ctx, network, host)
	if err != nil {
		if old, ok := lastGood.Load(key); ok {
			return old.([]string)
		}
		return nil
	}
	var out []string
	seen := map[string]bool{}
	for _, ip := range ips {
		a, ok := netip.AddrFromSlice(ip)
		if a = a.Unmap(); !ok || a.Is4() != (network == "ip4") || seen[a.String()] {
			continue
		}
		seen[a.String()] = true
		out = append(out, a.String())
	}
	sort.Strings(out)
	lastGood.Store(key, out)
	return out
}

// fieldAfter: значение после слова-ключа в строке ip route ("via 10.0.0.1", "dev eth0").
func fieldAfter(f []string, key string) string {
	for i := 0; i+1 < len(f); i++ {
		if f[i] == key {
			return f[i+1]
		}
	}
	return ""
}
