package netinfo

import (
	"errors"
	"net"
	"net/netip"
	"strconv"
	"strings"

	"golang.org/x/net/idna"
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
