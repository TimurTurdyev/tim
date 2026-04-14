package cmd

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/TimurTurdyev/tim/internal/config"
	"github.com/TimurTurdyev/tim/internal/domaudit"
	"github.com/TimurTurdyev/tim/internal/netinfo"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// tim vpn unblock <сайт>: сайт мимо VPN целиком, одной командой. Сначала сам сайт по владельцу
// сети, потом несколько кругов: открыть в браузере, посмотреть, в какие чужие сети он полез через
// туннель, пустить их мимо, открыть заново - пока новые не перестанут появляться. Плеер за
// редиректом, видео с другого хостинга, счётчики - всё всплывает само, угадывать по HTML не надо.

var (
	unblockRounds  int
	unblockSeconds int
	unblockYes     bool
	unblockAll     bool
)

var vpnUnblockCmd = &cobra.Command{
	Use:   "unblock <сайт> [--rounds 3] [--seconds 15] [-y]",
	Short: "Открыть сайт мимо VPN целиком: сам сайт, его плеер и всё, что он подгружает",
	Long: `Сайт, который пишет "отключите VPN", обычно грузит плеер, видео и скрипты с чужих сетей, и
обхода одного адреса мало. tim пускает мимо VPN сети владельца сайта, открывает сайт в браузере,
смотрит, куда тот ходит через туннель, добавляет эти сети и открывает сайт заново - до трёх кругов,
пока новые сети не кончатся. Общие сети (Cloudflare, Google, Amazon) не трогает: через них ходит
половина интернета. Убрать всё, что добавлено для сайта: tim route rm <сайт>.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error { return vpnUnblock(args[0]) },
}

// browserWords: по ним узнаём браузер среди процессов; остальное во время круга - шум.
var browserWords = []string{"firefox", "chrome", "chromium", "safari", "webkit", "brave", "edge", "opera",
	"vivaldi", "arc", "yandex", "orion", "zen", "librewolf", "epiphany", "falkon"}

func browserProc(name string) bool {
	l := strings.ToLower(name)
	for _, w := range browserWords {
		if strings.Contains(l, w) {
			return true
		}
	}
	return false
}

func vpnUnblock(site string) error {
	url := site
	if !strings.Contains(url, "://") {
		url = "https://" + url
	}
	host, _ := netinfo.HostPort(url, "")
	if host == "" || !strings.Contains(host, ".") {
		return ui.Errorf("нужен адрес сайта: tim vpn unblock example.com")
	}
	if len(netinfo.Tunnels()) == 0 {
		return ui.Errorf("туннеля нет: VPN выключен, сайт и так идёт напрямую")
	}
	known := map[string]bool{}
	for _, h := range cfg.Hosts {
		known[h.Addr] = true
	}
	var added []string
	add := func(targets []netinfo.Target, names []string) []string {
		var dests []string
		for i, t := range targets {
			if known[t.Value] {
				continue
			}
			known[t.Value] = true
			if cfg.AddHost(config.Host{Addr: t.Value, Label: host}) {
				added = append(added, t.Value)
				note := ""
				if i < len(names) {
					note = "  " + names[i]
				}
				ui.Println(ui.Label("мимо VPN ") + t.Value + ui.Dim(note))
			}
			dests = append(dests, expand(t)...)
		}
		return dests
	}
	apply := func(dests []string) error {
		if len(dests) == 0 {
			return nil
		}
		if err := saveConfig(cfg); err != nil {
			return err
		}
		return routeUp(dests)
	}

	// Круг 0: сам сайт по владельцу сети; на CDN - хотя бы его адреса.
	targets, names, err := ownerTargets(host, false)
	if err != nil {
		ui.Println(ui.Dim(err.Error()))
		targets, names = []netinfo.Target{{Kind: netinfo.KindDomain, Value: host}}, []string{"адреса домена"}
	}
	if err := apply(add(targets, names)); err != nil {
		return err
	}

	var cdn []*asGroup
	for round := 1; round <= unblockRounds; round++ {
		// Что уже открыто до сайта, к нему не относится.
		base := map[string]bool{}
		for _, p := range netinfo.Peers() {
			base[p.Remote] = true
		}
		if err := runner.OpenBrowser(url); err != nil {
			return err
		}
		note := fmt.Sprintf("круг %d из %d: сайт открыт в браузере, если это видео - нажми play", round, unblockRounds)
		seen, err := watchTunnel(unblockSeconds, base, note)
		if err != nil {
			return err
		}
		if !unblockAll {
			for ip, procs := range seen {
				mine := false
				for p := range procs {
					mine = mine || browserProc(p)
				}
				if !mine {
					delete(seen, ip)
				}
			}
		}
		var fresh []*asGroup
		for _, g := range groupByAS(seen) {
			switch {
			case known["as:"+g.as.ASN]:
			case domaudit.IsCDN(g.as.Name):
				cdn = append(cdn, g)
			default:
				fresh = append(fresh, g)
			}
		}
		if len(fresh) == 0 {
			ui.Println(ui.Dim("новых сетей не появилось"))
			break
		}
		ui.Println(ui.Dim(fmt.Sprintf("за круг сайт сходил в %d %s:", len(fresh), ui.Plural(len(fresh), "новую сеть", "новые сети", "новых сетей"))))
		var targets []netinfo.Target
		var names []string
		for _, g := range fresh {
			hint := g.procList()
			if len(g.names) > 0 {
				hint += "  " + strings.Join(g.names, " ")
			}
			ui.Println("  " + ui.Label(ui.Pad("as:"+g.as.ASN, 10)) + ui.Pad(ui.Clip(g.as.Name, 36), 37) + ui.Pad(strconv.Itoa(len(g.ips)), 4) + ui.Dim(ui.Clip(hint, 50)))
			targets = append(targets, netinfo.Target{Kind: netinfo.KindAS, Value: "as:" + g.as.ASN})
			names = append(names, g.as.Name)
		}
		if !unblockYes && !runner.ConfirmDefault("пустить их мимо VPN?", true) {
			break
		}
		if err := apply(add(targets, names)); err != nil {
			return err
		}
	}
	ui.Println()
	switch {
	case len(added) == 0:
		ui.Println(ui.Dim("ничего не добавлено: сайт и так идёт мимо VPN или не ходил в туннель"))
	default:
		ui.Println(ui.Label(host) + ui.Dim(fmt.Sprintf("  мимо VPN идут %d %s; убрать разом: tim route rm %s",
			len(added), ui.Plural(len(added), "сеть", "сети", "сетей"), host)))
		_ = runner.OpenBrowser(url)
	}
	if len(cdn) > 0 {
		var list []string
		for _, g := range cdn {
			list = append(list, "as:"+g.as.ASN+" "+ui.Clip(g.as.Name, 24))
		}
		ui.Println(ui.Dim("ещё сайт ходил в общие сети, их мимо VPN не пускаю: " + strings.Join(list, "; ")))
		ui.Println(ui.Dim("если без них не работает: tim vpn exclude as:N --force, но через них ходит и всё остальное"))
	}
	return nil
}

func init() {
	vpnUnblockCmd.Flags().IntVar(&unblockRounds, "rounds", 3, "сколько кругов смотреть, куда ходит сайт")
	vpnUnblockCmd.Flags().IntVar(&unblockSeconds, "seconds", 15, "сколько секунд смотреть за круг")
	vpnUnblockCmd.Flags().BoolVarP(&unblockYes, "yes", "y", false, "не спрашивать после каждого круга")
	vpnUnblockCmd.Flags().BoolVar(&unblockAll, "all", false, "учитывать все программы, а не только браузер")
	vpnCmd.AddCommand(vpnUnblockCmd)
}
