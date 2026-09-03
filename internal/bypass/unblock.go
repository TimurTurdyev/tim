package bypass

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/TimurTurdyev/tim/internal/app"
	"github.com/TimurTurdyev/tim/internal/asn"
	"github.com/TimurTurdyev/tim/internal/config"
	"github.com/TimurTurdyev/tim/internal/netinfo"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// Сайт мимо VPN целиком, одной командой. Сначала сам сайт по владельцу сети, потом несколько кругов:
// открыть в браузере, посмотреть, в какие чужие сети он полез через туннель, пустить их мимо,
// открыть заново - пока новые не перестанут появляться. Плеер за редиректом, видео с другого
// хостинга, счётчики - всё всплывает само, угадывать по HTML не надо.

// browserWords: по ним узнаём браузер среди процессов; остальное во время круга - шум.
var browserWords = []string{"firefox", "chrome", "chromium", "safari", "webkit", "brave", "edge", "opera",
	"vivaldi", "arc", "yandex", "orion", "zen", "librewolf", "epiphany", "falkon"}

// unblocking: цели, добавленные ради одного сайта, и уже известные - чтобы не добавить дважды.
type unblocking struct {
	env   *app.Env
	host  string
	known map[string]bool
	added []string
}

func unblock(env *app.Env, site string, o unblockOptions) error {
	url := site
	if !strings.Contains(url, "://") {
		url = "https://" + url
	}
	host, _ := netinfo.HostPort(url, "")
	if host == "" || !strings.Contains(host, ".") {
		return ui.Errorf("нужен адрес сайта: tim direct example.org")
	}
	if len(netinfo.Tunnels()) == 0 {
		return ui.Errorf("туннеля нет: VPN выключен, сайт и так идёт напрямую")
	}
	u := &unblocking{env: env, host: host, known: map[string]bool{}}
	for _, h := range env.Cfg.Hosts {
		u.known[h.Addr] = true
	}
	// Круг 0: сам сайт по владельцу сети; на CDN - хотя бы его адреса.
	targets, names, err := OwnerTargets(host, false)
	if err != nil {
		ui.Println(ui.Dim(err.Error()))
		targets, names = []netinfo.Target{{Kind: netinfo.KindDomain, Value: host}}, []string{"адреса домена"}
	}
	if err := u.apply(targets, names); err != nil {
		return err
	}
	var cdn []*Group
	for round := 1; round <= o.rounds; round++ {
		fresh, shared, err := u.round(url, round, o)
		if err != nil {
			return err
		}
		cdn = append(cdn, shared...)
		if len(fresh) == 0 {
			ui.Println(ui.Dim("новых сетей не появилось"))
			break
		}
		targets, names := offerGroups(fresh)
		if !o.yes && !runner.ConfirmDefault("пустить их мимо VPN?", true) {
			break
		}
		if err := u.apply(targets, names); err != nil {
			return err
		}
	}
	u.report(url, cdn)
	return nil
}

// round: открыть сайт и посмотреть, в какие сети он сходил через туннель. fresh - новые сети,
// shared - общие (CDN, облака), их мимо VPN не пускаем.
func (u *unblocking) round(url string, n int, o unblockOptions) (fresh, shared []*Group, err error) {
	// Что уже открыто до сайта, к нему не относится.
	base := map[string]bool{}
	for _, p := range netinfo.Peers() {
		base[p.Remote] = true
	}
	if err := runner.OpenBrowser(url); err != nil {
		return nil, nil, err
	}
	note := fmt.Sprintf("круг %d из %d: сайт открыт в браузере, если это видео - нажми play", n, o.rounds)
	seen, err := WatchTunnel(o.seconds, base, note)
	if err != nil {
		return nil, nil, err
	}
	if !o.all {
		keepBrowsers(seen)
	}
	for _, g := range GroupByAS(seen) {
		switch {
		case u.known["as:"+g.AS.ASN]:
		case asn.IsCDN(g.AS.Name):
			shared = append(shared, g)
		default:
			fresh = append(fresh, g)
		}
	}
	return fresh, shared, nil
}

// apply: новые цели в конфиг с пометкой сайта и маршруты к ним.
func (u *unblocking) apply(targets []netinfo.Target, names []string) error {
	var dests []string
	for i, t := range targets {
		if u.known[t.Value] {
			continue
		}
		u.known[t.Value] = true
		if u.env.Cfg.AddHost(config.Host{Addr: t.Value, Label: u.host}) {
			u.added = append(u.added, t.Value)
			note := ""
			if i < len(names) {
				note = "  " + names[i]
			}
			ui.Println(ui.Label("мимо VPN ") + t.Value + ui.Dim(note))
		}
		dests = append(dests, Expand(t)...)
	}
	if len(dests) == 0 {
		return nil
	}
	if err := u.env.Save(); err != nil {
		return err
	}
	return Up(u.env.Cfg, dests)
}

func (u *unblocking) report(url string, cdn []*Group) {
	ui.Println()
	if len(u.added) == 0 {
		ui.Println(ui.Dim("ничего не добавлено: сайт и так идёт мимо VPN или не ходил в туннель"))
	} else {
		ui.Println(ui.Label(u.host) + ui.Dim(fmt.Sprintf("  мимо VPN идут %d %s; убрать разом: tim direct rm %s",
			len(u.added), ui.Plural(len(u.added), "сеть", "сети", "сетей"), u.host)))
		_ = runner.OpenBrowser(url)
	}
	if len(cdn) == 0 {
		return
	}
	var list []string
	for _, g := range cdn {
		list = append(list, "as:"+g.AS.ASN+" "+ui.Clip(g.AS.Name, 24))
	}
	ui.Println(ui.Dim("ещё сайт ходил в общие сети, их мимо VPN не пускаю: " + strings.Join(list, "; ")))
	ui.Println(ui.Dim("если без них не работает: tim direct as:N --force, но через них ходит и всё остальное"))
}

// offerGroups печатает новые сети круга и отдаёт их целями.
func offerGroups(fresh []*Group) ([]netinfo.Target, []string) {
	ui.Println(ui.Dim(fmt.Sprintf("за круг сайт сходил в %s:", ui.Count(len(fresh), "новую сеть", "новые сети", "новых сетей"))))
	var targets []netinfo.Target
	var names []string
	for _, g := range fresh {
		hint := g.Procs()
		if len(g.Names) > 0 {
			hint += "  " + strings.Join(g.Names, " ")
		}
		ui.Println("  " + ui.Label(ui.Pad("as:"+g.AS.ASN, 10)) + ui.Pad(ui.Clip(g.AS.Name, 36), 37) + ui.Pad(strconv.Itoa(len(g.IPs)), 4) + ui.Dim(ui.Clip(hint, 50)))
		targets = append(targets, netinfo.Target{Kind: netinfo.KindAS, Value: "as:" + g.AS.ASN})
		names = append(names, g.AS.Name)
	}
	return targets, names
}

// keepBrowsers: оставить адреса, куда ходил браузер; остальные программы во время круга - шум.
func keepBrowsers(seen map[string]map[string]bool) {
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

func browserProc(name string) bool {
	l := strings.ToLower(name)
	for _, w := range browserWords {
		if strings.Contains(l, w) {
			return true
		}
	}
	return false
}
