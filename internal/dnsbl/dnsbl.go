// Package dnsbl: адрес в чёрных списках почты по DNS. Spamhaus не отвечает на запросы через публичные
// резолверы (1.1.1.1, 8.8.8.8): вместо ответа отдаёт 127.255.255.x, это показывается честно, а не как "чисто".
package dnsbl

import (
	"context"
	"errors"
	"net"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Zones: списки, которые смотрят почтовые серверы чаще всего.
var Zones = []string{"zen.spamhaus.org", "b.barracudacentral.org", "bl.spamcop.net"}

// Result: ответ одного списка.
type Result struct {
	Zone    string `json:"zone"`
	Listed  bool   `json:"listed"`
	Unknown string `json:"unknown,omitempty"` // почему ответа нет
}

// Check: IPv4 по всем спискам; не IPv4 - пусто.
func Check(ip string) []Result {
	parsed := net.ParseIP(ip).To4()
	if parsed == nil {
		return nil
	}
	rev := []string{}
	for i := 3; i >= 0; i-- {
		rev = append(rev, strconv.Itoa(int(parsed[i])))
	}
	out := make([]Result, len(Zones))
	var wg sync.WaitGroup
	for i, z := range Zones {
		wg.Go(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
			defer cancel()
			addrs, err := net.DefaultResolver.LookupHost(ctx, strings.Join(rev, ".")+"."+z)
			out[i] = classify(z, addrs, err)
		})
	}
	wg.Wait()
	return out
}

func classify(zone string, addrs []string, err error) Result {
	r := Result{Zone: zone}
	var dnsErr *net.DNSError
	switch {
	case err != nil && errors.As(err, &dnsErr) && dnsErr.IsNotFound:
	case err != nil:
		r.Unknown = "не ответил"
	case slices.ContainsFunc(addrs, func(a string) bool { return strings.HasPrefix(a, "127.255.255.") }):
		r.Unknown = "не отвечает через публичный DNS-резолвер"
	default:
		r.Listed = true
	}
	return r
}

// Resolve: IPv4 хоста - имя или уже адрес.
func Resolve(host string) string {
	if ip := net.ParseIP(host); ip != nil {
		return host
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	ips, err := net.DefaultResolver.LookupIP(ctx, "ip4", host)
	if err != nil || len(ips) == 0 {
		return ""
	}
	return ips[0].String()
}
