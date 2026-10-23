package cmd

import (
	"context"
	"fmt"
	"net"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/TimurTurdyev/tim/internal/config"
	"github.com/TimurTurdyev/tim/internal/domaudit"
	"github.com/TimurTurdyev/tim/internal/netinfo"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/tui"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// Обход по наблюдению: сайт грузит плеер, CDN и счётчики с десятка чужих сетей, и угадать их по
// HTML нельзя. tim смотрит, какие соединения ушли в туннель, пока человек открывает сайт,
// складывает адреса по владельцам сетей и предлагает пустить выбранные мимо VPN.

type asGroup struct {
	as    domaudit.ASInfo
	ips   []string
	procs map[string]bool
	names []string // обратные имена - подсказка, что это за сеть
}

// watchTunnel собирает адреса, в которые за seconds секунд ходили через туннель.
func watchTunnel(seconds int) (map[string]map[string]bool, error) {
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
			if !tunnelIPs[p.Local] || !netinfo.IsIPv4(p.Remote) || netinfo.IsPrivate(p.Remote) {
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
		if stdoutTerminal() {
			fmt.Fprintf(os.Stderr, "\r\033[2K"+ui.Dim("смотрю в туннель ещё %d с: открой сайт или программу, которая не работает; адресов %d"),
				int(left.Seconds())+1, len(seen))
		}
		time.Sleep(time.Second)
	}
	if stdoutTerminal() {
		fmt.Fprint(os.Stderr, "\r\033[2K")
	}
	return seen, nil
}

// groupByAS складывает адреса по владельцу сети; у каждой группы пара обратных имён для подсказки.
func groupByAS(seen map[string]map[string]bool) []*asGroup {
	ips := make([]string, 0, len(seen))
	for ip := range seen {
		ips = append(ips, ip)
	}
	sort.Strings(ips)
	info := domaudit.Cymru(ips)
	groups := map[string]*asGroup{}
	for _, ip := range ips {
		a, ok := info[ip]
		if !ok || a.ASN == "" {
			continue
		}
		g := groups[a.ASN]
		if g == nil {
			g = &asGroup{as: a, procs: map[string]bool{}}
			groups[a.ASN] = g
		}
		g.ips = append(g.ips, ip)
		for p := range seen[ip] {
			g.procs[p] = true
		}
	}
	out := make([]*asGroup, 0, len(groups))
	for _, g := range groups {
		out = append(out, g)
	}
	sort.Slice(out, func(i, j int) bool {
		if len(out[i].ips) != len(out[j].ips) {
			return len(out[i].ips) > len(out[j].ips)
		}
		return out[i].as.ASN < out[j].as.ASN
	})
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	for _, g := range out {
		for _, ip := range g.ips[:min(2, len(g.ips))] {
			if names, err := net.DefaultResolver.LookupAddr(ctx, ip); err == nil && len(names) > 0 {
				g.names = append(g.names, strings.TrimSuffix(names[0], "."))
			}
		}
	}
	return out
}

func (g *asGroup) procList() string {
	var list []string
	for p := range g.procs {
		list = append(list, p)
	}
	sort.Strings(list)
	return strings.Join(list, ", ")
}

// vpnWatch: посмотреть, выбрать, завести маршруты. label - пометка целей, обычно сайт, ради которого всё.
func vpnWatch(seconds int, label string, o addOpts) error {
	seen, err := watchTunnel(seconds)
	if err != nil {
		return err
	}
	if len(seen) == 0 {
		ui.Println(ui.Dim("за это время в туннель никто не ходил"))
		return nil
	}
	groups := groupByAS(seen)
	if len(groups) == 0 {
		return ui.Errorf("владельцев сетей не узнать: whois.cymru.com не ответил")
	}
	ui.Println(ui.Dim(ui.Pad("СЕТЬ", 10) + ui.Pad("ВЛАДЕЛЕЦ", 34) + ui.Pad("АДРЕСОВ", 9) + "КТО ХОДИЛ, ИМЕНА"))
	items := make([]tui.Item, len(groups))
	sel := make([]bool, len(groups))
	for i, g := range groups {
		owner := g.as.Name
		// Имя из whois обычно уже кончается страной: "..., US".
		if !strings.HasSuffix(owner, ", "+g.as.Country) {
			owner = strings.TrimSpace(owner + " " + g.as.Country)
		}
		hint := g.procList()
		if len(g.names) > 0 {
			hint += "  " + strings.Join(g.names, " ")
		}
		cdn := domaudit.IsCDN(g.as.Name) && !o.force
		line := ui.Label(ui.Pad("as:"+g.as.ASN, 10)) + ui.Pad(clip(owner, 33), 34) + ui.Pad(strconv.Itoa(len(g.ips)), 9) + ui.Dim(clip(hint, 60))
		if cdn {
			line += ui.Warn("  общая сеть, мимо VPN целиком не стоит")
		}
		ui.Println(line)
		items[i] = tui.Item{Title: "as:" + g.as.ASN + "  " + clip(owner, 30), Hint: hint}
		sel[i] = !cdn
	}
	var chosen []int
	switch {
	case !tui.Available():
		var cmd []string
		for i, g := range groups {
			if sel[i] {
				cmd = append(cmd, "as:"+g.as.ASN)
			}
		}
		if len(cmd) > 0 {
			ui.Println(ui.Dim("пустить мимо VPN: tim vpn exclude " + strings.Join(cmd, " ") + labelFlag(label)))
		}
		return nil
	default:
		ui.Println()
		chosen, err = tui.MultiSelect("Какие сети пустить мимо VPN?", items, sel)
		if err != nil {
			return pickErr(err, "tim vpn exclude as:N", items)
		}
	}
	if len(chosen) == 0 {
		return nil
	}
	var dests []string
	for _, i := range chosen {
		g := groups[i]
		t, err := netinfo.ParseTarget("as:" + g.as.ASN)
		if err != nil {
			return err
		}
		if cfg.AddHost(config.Host{Addr: t.Value, Label: label}) {
			ui.Println(ui.Label("добавлен ") + t.Value + ui.Dim("  "+g.as.Name))
		} else {
			ui.Println(ui.Dim(t.Value + ": уже в списке"))
		}
		dests = append(dests, expand(t)...)
	}
	if err := saveConfig(cfg); err != nil {
		return err
	}
	if o.noUp || runner.DryRun && len(dests) == 0 {
		return nil
	}
	return routeUp(dests)
}

func labelFlag(label string) string {
	if label == "" {
		return ""
	}
	return " --label " + label
}
