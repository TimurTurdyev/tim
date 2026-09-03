package bypass

import (
	"strconv"
	"strings"

	"github.com/TimurTurdyev/tim/internal/app"
	"github.com/TimurTurdyev/tim/internal/asn"
	"github.com/TimurTurdyev/tim/internal/config"
	"github.com/TimurTurdyev/tim/internal/netinfo"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// Обход по наблюдению: сайт грузит плеер, CDN и счётчики с десятка чужих сетей, и угадать их по
// HTML нельзя. tim смотрит, какие соединения ушли в туннель, пока человек открывает сайт,
// складывает адреса по владельцам сетей и предлагает пустить выбранные мимо VPN.

// chooseNets: посмотреть, выбрать, завести маршруты. label - пометка целей, обычно сайт, ради которого всё.
func chooseNets(env *app.Env, seconds int, label string, o addOptions) error {
	seen, err := WatchTunnel(seconds, nil, "смотрю в туннель: открой сайт или программу, которая не работает")
	if err != nil {
		return err
	}
	if len(seen) == 0 {
		ui.Println(ui.Dim("за это время в туннель никто не ходил"))
		return nil
	}
	groups := GroupByAS(seen)
	if len(groups) == 0 {
		return ui.Errorf("владельцев сетей не узнать: whois.cymru.com не ответил")
	}
	opts, sel := printGroups(groups, o.force)
	if !app.Interactive() {
		suggestExclude(groups, sel, label)
		return nil
	}
	ui.Println()
	chosen, err := app.ChooseManyFrom("Какие сети пустить мимо VPN?", opts, sel)
	if err != nil {
		return app.PickErr(err, "tim direct as:N", nil)
	}
	if len(chosen) == 0 {
		return nil
	}
	var dests []string
	for _, i := range chosen {
		t := netinfo.Target{Kind: netinfo.KindAS, Value: "as:" + groups[i].AS.ASN}
		if env.Cfg.AddHost(config.Host{Addr: t.Value, Label: label}) {
			ui.Println(ui.Label("добавлен ") + t.Value + ui.Dim("  "+groups[i].AS.Name))
		} else {
			ui.Println(ui.Dim(t.Value + ": уже в списке"))
		}
		dests = append(dests, Expand(t)...)
	}
	if err := env.Save(); err != nil {
		return err
	}
	if o.noUp || runner.DryRun && len(dests) == 0 {
		return nil
	}
	return Up(env.Cfg, dests)
}

// printGroups печатает сети, куда ходили через туннель; общие сети заранее не отмечены.
func printGroups(groups []*Group, force bool) ([]app.Option, []bool) {
	ui.Println(ui.Dim(ui.Pad("СЕТЬ", 10) + ui.Pad("ВЛАДЕЛЕЦ", 34) + ui.Pad("АДРЕСОВ", 9) + "КТО ХОДИЛ, ИМЕНА"))
	opts := make([]app.Option, len(groups))
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
		cdn := asn.IsCDN(g.AS.Name) && !force
		line := ui.Label(ui.Pad("as:"+g.AS.ASN, 10)) + ui.Pad(ui.Clip(owner, 33), 34) + ui.Pad(strconv.Itoa(len(g.IPs)), 9) + ui.Dim(ui.Clip(hint, 60))
		if cdn {
			line += ui.Warn("  общая сеть, мимо VPN целиком не стоит")
		}
		ui.Println(line)
		opts[i] = app.Option{Title: "as:" + g.AS.ASN + "  " + ui.Clip(owner, 30), Hint: hint}
		sel[i] = !cdn
	}
	return opts, sel
}

// suggestExclude: без терминала выбирать некому - готовая команда с отмеченными сетями.
func suggestExclude(groups []*Group, sel []bool, label string) {
	var nets []string
	for i, g := range groups {
		if sel[i] {
			nets = append(nets, "as:"+g.AS.ASN)
		}
	}
	if len(nets) == 0 {
		return
	}
	line := "пустить мимо VPN: tim direct " + strings.Join(nets, " ")
	if label != "" {
		line += " --label " + label
	}
	ui.Println(ui.Dim(line))
}

// watchLabel: пометка целей при --watch - явная или сам аргумент, обычно сайт.
func watchLabel(o addOptions, args []string) string {
	if o.label == "" && len(args) == 1 {
		return args[0]
	}
	return o.label
}
