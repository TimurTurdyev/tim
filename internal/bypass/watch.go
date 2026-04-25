package bypass

import (
	"context"
	"fmt"
	"net"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/TimurTurdyev/tim/internal/domaudit"
	"github.com/TimurTurdyev/tim/internal/netinfo"
	"github.com/TimurTurdyev/tim/internal/ui"
)

type Group struct {
	AS    domaudit.ASInfo
	IPs   []string
	procs map[string]bool
	Names []string // обратные имена - подсказка, что это за сеть
}

// WatchTunnel собирает адреса, в которые за seconds секунд ходили через туннель; skip - адреса,
// открытые заранее, они к делу не относятся; note - что сказать человеку, пока идёт отсчёт.
func WatchTunnel(seconds int, skip map[string]bool, note string) (map[string]map[string]bool, error) {
	tunnelIPs := map[string]bool{}
	for _, t := range netinfo.Tunnels() {
		for _, ip := range t.IPv4 {
			tunnelIPs[ip] = true
		}
	}
	if len(tunnelIPs) == 0 {
		return nil, ui.Errorf("туннеля нет: VPN выключен, смотреть нечего")
	}
	seen := map[string]map[string]bool{}
	deadline := time.Now().Add(time.Duration(seconds) * time.Second)
	for {
		for _, p := range netinfo.Peers() {
			if !tunnelIPs[p.Local] || !netinfo.IsIPv4(p.Remote) || netinfo.IsPrivate(p.Remote) || skip[p.Remote] {
				continue
			}
			if seen[p.Remote] == nil {
				seen[p.Remote] = map[string]bool{}
			}
			seen[p.Remote][p.Proc] = true
		}
		left := time.Until(deadline)
		if left <= 0 {
			break
		}
		if ui.Terminal() {
			fmt.Fprintf(os.Stderr, "\r\033[2K"+ui.Dim("%s; ещё %d с, адресов в туннеле %d"), note, int(left.Seconds())+1, len(seen))
		}
		time.Sleep(time.Second)
	}
	if ui.Terminal() {
		fmt.Fprint(os.Stderr, "\r\033[2K")
	}
	return seen, nil
}

// GroupByAS складывает адреса по владельцу сети; у каждой группы пара обратных имён для подсказки.
func GroupByAS(seen map[string]map[string]bool) []*Group {
	ips := make([]string, 0, len(seen))
	for ip := range seen {
		ips = append(ips, ip)
	}
	sort.Strings(ips)
	info := domaudit.Cymru(ips)
	groups := map[string]*Group{}
	for _, ip := range ips {
		a, ok := info[ip]
		if !ok || a.ASN == "" {
			continue
		}
		g := groups[a.ASN]
		if g == nil {
			g = &Group{AS: a, procs: map[string]bool{}}
			groups[a.ASN] = g
		}
		g.IPs = append(g.IPs, ip)
		for p := range seen[ip] {
			g.procs[p] = true
		}
	}
	out := make([]*Group, 0, len(groups))
	for _, g := range groups {
		out = append(out, g)
	}
	sort.Slice(out, func(i, j int) bool {
		if len(out[i].IPs) != len(out[j].IPs) {
			return len(out[i].IPs) > len(out[j].IPs)
		}
		return out[i].AS.ASN < out[j].AS.ASN
	})
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	for _, g := range out {
		for _, ip := range g.IPs[:min(2, len(g.IPs))] {
			if names, err := net.DefaultResolver.LookupAddr(ctx, ip); err == nil && len(names) > 0 {
				g.Names = append(g.Names, strings.TrimSuffix(names[0], "."))
			}
		}
	}
	return out
}

func (g *Group) Procs() string {
	var list []string
	for p := range g.procs {
		list = append(list, p)
	}
	sort.Strings(list)
	return strings.Join(list, ", ")
}
