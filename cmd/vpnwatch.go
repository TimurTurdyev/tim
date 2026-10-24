package cmd

import (
	"strconv"
	"strings"

	"github.com/TimurTurdyev/tim/internal/bypass"
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

// vpnWatch: посмотреть, выбрать, завести маршруты. label - пометка целей, обычно сайт, ради которого всё.
func vpnWatch(seconds int, label string, o addOpts) error {
	seen, err := bypass.WatchTunnel(seconds, nil, "смотрю в туннель: открой сайт или программу, которая не работает")
	if err != nil {
		return err
	}
	if len(seen) == 0 {
		ui.Println(ui.Dim("за это время в туннель никто не ходил"))
		return nil
	}
	groups := bypass.GroupByAS(seen)
	if len(groups) == 0 {
		return ui.Errorf("владельцев сетей не узнать: whois.cymru.com не ответил")
	}
	ui.Println(ui.Dim(ui.Pad("СЕТЬ", 10) + ui.Pad("ВЛАДЕЛЕЦ", 34) + ui.Pad("АДРЕСОВ", 9) + "КТО ХОДИЛ, ИМЕНА"))
	items := make([]tui.Item, len(groups))
	sel := make([]bool, len(groups))
	for i, g := range groups {
		owner := g.AS.Name
		// Имя из whois обычно уже кончается страной: "..., US".
		if !strings.HasSuffix(owner, ", "+g.AS.Country) {
			owner = strings.TrimSpace(owner + " " + g.AS.Country)
		}
		hint := g.Procs()
		if len(g.Names) > 0 {
			hint += "  " + strings.Join(g.Names, " ")
		}
		cdn := domaudit.IsCDN(g.AS.Name) && !o.force
		line := ui.Label(ui.Pad("as:"+g.AS.ASN, 10)) + ui.Pad(ui.Clip(owner, 33), 34) + ui.Pad(strconv.Itoa(len(g.IPs)), 9) + ui.Dim(ui.Clip(hint, 60))
		if cdn {
			line += ui.Warn("  общая сеть, мимо VPN целиком не стоит")
		}
		ui.Println(line)
		items[i] = tui.Item{Title: "as:" + g.AS.ASN + "  " + ui.Clip(owner, 30), Hint: hint}
		sel[i] = !cdn
	}
	var chosen []int
	switch {
	case !tui.Available():
		var cmd []string
		for i, g := range groups {
			if sel[i] {
				cmd = append(cmd, "as:"+g.AS.ASN)
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
		t, err := netinfo.ParseTarget("as:" + g.AS.ASN)
		if err != nil {
			return err
		}
		if cfg.AddHost(config.Host{Addr: t.Value, Label: label}) {
			ui.Println(ui.Label("добавлен ") + t.Value + ui.Dim("  "+g.AS.Name))
		} else {
			ui.Println(ui.Dim(t.Value + ": уже в списке"))
		}
		dests = append(dests, bypass.Expand(t)...)
	}
	if err := saveConfig(cfg); err != nil {
		return err
	}
	if o.noUp || runner.DryRun && len(dests) == 0 {
		return nil
	}
	return bypass.Up(cfg, dests)
}

func labelFlag(label string) string {
	if label == "" {
		return ""
	}
	return " --label " + label
}
