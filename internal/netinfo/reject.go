package netinfo

import (
	"net/netip"
	"runtime"
	"strings"

	"github.com/TimurTurdyev/tim/internal/runner"
)

// Закрытые сети IPv6: обход ведёт цели по IPv4, а их IPv6 закрывает маршрутом reject, иначе программа
// уйдёт к цели по IPv6 через туннель. Отказ приходит сразу, и программа переходит на IPv4.

// ParseV6: сеть или адрес IPv6 (адрес - как /128); у адресов с зоной (fe80::1%en0) своей сети нет.
func ParseV6(s string) (netip.Prefix, bool) {
	if p, err := netip.ParsePrefix(s); err == nil && p.Addr().Is6() && p.Addr().Zone() == "" {
		return p.Masked(), true
	}
	if a, err := netip.ParseAddr(s); err == nil && a.Is6() && !a.Is4In6() && a.Zone() == "" {
		return netip.PrefixFrom(a, 128), true
	}
	return netip.Prefix{}, false
}

// ParseRejects: закрытые сети из netstat -rn -f inet6 - строки с флагом R.
func ParseRejects(out string) map[netip.Prefix]bool {
	got := map[netip.Prefix]bool{}
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) < 4 || !strings.Contains(f[2], "R") {
			continue
		}
		if p, ok := ParseV6(f[0]); ok {
			got[p] = true
		}
	}
	return got
}

// ParseIPRejects: закрытые сети из ip -6 route show - строки unreachable.
func ParseIPRejects(out string) map[netip.Prefix]bool {
	got := map[netip.Prefix]bool{}
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) < 2 || f[0] != "unreachable" {
			continue
		}
		if p, ok := ParseV6(f[1]); ok {
			got[p] = true
		}
	}
	return got
}

// Rejects: закрытые сети IPv6 из таблицы маршрутов.
func Rejects() map[netip.Prefix]bool {
	if runtime.GOOS == "linux" {
		out, _ := runner.Output("ip", "-6", "route", "show")
		return ParseIPRejects(out)
	}
	out, _ := runner.Output("netstat", "-rn", "-f", "inet6")
	return ParseRejects(out)
}

// RejectCmd: команда, которая закрывает сеть IPv6.
func RejectCmd(p netip.Prefix) []string {
	if runtime.GOOS == "linux" {
		return []string{"ip", "-6", "route", "replace", "unreachable", p.String()}
	}
	return append(append([]string{"route", "-n", "add", "-inet6"}, dest6(p)...), "::1", "-reject")
}

// UnrejectCmd: команда, которая снимает закрытие.
func UnrejectCmd(p netip.Prefix) []string {
	if runtime.GOOS == "linux" {
		return []string{"ip", "-6", "route", "del", "unreachable", p.String()}
	}
	return append([]string{"route", "-n", "delete", "-inet6"}, dest6(p)...)
}

func dest6(p netip.Prefix) []string {
	if p.IsSingleIP() {
		return []string{"-host", p.Addr().String()}
	}
	return []string{"-net", p.String()}
}
