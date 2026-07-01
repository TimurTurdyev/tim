package bypass

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/TimurTurdyev/tim/internal/config"
	"github.com/TimurTurdyev/tim/internal/netinfo"
	"github.com/TimurTurdyev/tim/internal/routewatch"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// printRoutes: интерфейс, куда идёт трафик, страж и состояние каждой цели; probe - проверить порт 22.
func printRoutes(cfg *config.Config, probe bool) {
	phys, gw := netinfo.Physical()
	if phys.Device == "" {
		ui.Println(ui.Warn("физический интерфейс не найден: сеть выключена?"))
	} else {
		ui.Println(ui.Dim("физический интерфейс ") + phys.Device + ui.Dim(" шлюз ") + gw)
	}
	inet := netinfo.InternetIface()
	line := ui.Dim("трафик в интернет идёт через ") + ui.Dash(inet)
	if netinfo.IsTunnelIface(inet) {
		line += "  " + ui.Warn("(VPN забирает весь трафик)")
	}
	ui.Println(line)
	if len(cfg.Hosts) == 0 {
		ui.Println(ui.Dim("целей обхода нет: tim route add <адрес|домен|подсеть>"))
		return
	}
	ui.Println(GuardLine())
	ui.Println(ui.Dim(ui.Pad("ЦЕЛЬ", 24) + " " + ui.Pad("ПОМЕТКА", 11) + " " + ui.Pad("АДРЕС", 18) + " СОСТОЯНИЕ"))
	for _, h := range cfg.Hosts {
		dests := ExpandAddr(h.Addr)
		if len(dests) == 0 {
			ui.Println(ui.Pad(h.Addr, 24) + " " + ui.Pad(h.Label, 11) + " " + ui.Bad("не раскрывается в адреса"))
			continue
		}
		for i, d := range dests {
			name, label := h.Addr, h.Label
			if i > 0 {
				name, label = "", ""
			}
			state, _ := RouteState(d, phys, gw)
			if probe && !strings.Contains(d, "/") {
				state += "  " + portNote(d)
			}
			ui.Println(ui.Pad(name, 24) + " " + ui.Pad(label, 11) + " " + ui.Pad(d, 18) + " " + state)
		}
	}
}

func portNote(addr string) string {
	if PortOpen(addr, "22") {
		return ui.Label("порт 22 отвечает")
	}
	return ui.Bad("порт 22 молчит")
}

// printGuard: служба стража, последняя проверка, починки и их причины.
func printGuard() {
	installed, running := GuardInstalled(), GuardRunning()
	switch {
	case !installed:
		ui.Println(ui.Warn("служба не установлена") + ui.Dim("  sudo tim route install"))
	case running:
		ui.Println(ui.Label("служба работает") + ui.Dim("  журнал "+GuardLog()))
	default:
		ui.Println(ui.Bad("служба установлена, но не работает") + ui.Dim("  журнал "+GuardLog()))
	}
	st, ok := GuardStatus()
	if !ok {
		ui.Println(ui.Dim("отчёта стража нет"))
		return
	}
	now := time.Now()
	ui.Println(ui.Label("последняя проверка ") + ui.Duration(now.Sub(st.LastCheck)) + " назад" +
		ui.Dim(", запущен "+st.Started.Format("02.01 15:04")))
	ui.Println(ui.Label("сеть               ") + st.Iface + " шлюз " + ui.Dash(st.Gateway))
	fine := strconv.Itoa(st.OK) + " из " + strconv.Itoa(st.Want)
	if st.OK < st.Want {
		fine = ui.Warn(fine)
	}
	ui.Println(ui.Label("маршрутов в порядке ") + fine)
	if st.Error != "" {
		ui.Println(ui.Bad("ошибка: " + st.Error))
	}
	day, week := st.Since(now.Add(-24*time.Hour)), st.Since(now.AddDate(0, 0, -7))
	ui.Println(ui.Label("починок            ") + fmt.Sprintf("за сутки %d, за неделю %d", day, week))
	if len(st.Repairs) == 0 {
		return
	}
	printRepairs(st.Repairs, now)
}

func printRepairs(repairs []routewatch.Repair, now time.Time) {
	var recent []routewatch.Repair
	for _, r := range repairs {
		if r.At.After(now.AddDate(0, 0, -7)) {
			recent = append(recent, r)
		}
	}
	sum := routewatch.Summary(recent)
	kinds := make([]string, 0, len(sum))
	for k := range sum {
		kinds = append(kinds, k)
	}
	sort.Slice(kinds, func(i, j int) bool {
		if sum[kinds[i]] != sum[kinds[j]] {
			return sum[kinds[i]] > sum[kinds[j]]
		}
		return kinds[i] < kinds[j]
	})
	for _, k := range kinds {
		ui.Println(ui.Dim("  " + ui.Pad(strconv.Itoa(sum[k]), 5) + k))
	}
	ui.Println(ui.Dim("последние:"))
	for _, r := range repairs[max(0, len(repairs)-5):] {
		ui.Println(ui.Dim("  " + r.At.Format("02.01 15:04") + "  " + ui.Pad(r.Dest, 18) + " " + r.Reason))
	}
}

// printVPN: туннели, подключения, куда идёт интернет, DNS и сколько целей идут мимо VPN.
func printVPN(cfg *config.Config) {
	tunnels := netinfo.Tunnels()
	hasV6 := false
	if len(tunnels) == 0 {
		ui.Println(ui.Dim("туннелей нет"))
	}
	for _, t := range tunnels {
		addrs := append(append([]string{}, t.IPv4...), t.IPv6...)
		hasV6 = hasV6 || len(t.IPv6) > 0
		ui.Println(ui.Label(ui.Pad(t.Iface, 8)) + strings.Join(addrs, " "))
	}
	for _, c := range netinfo.NetConns() {
		if c.State == "Connected" {
			ui.Println(ui.Label("подключение ") + c.Name)
		}
	}
	inet := netinfo.InternetIface()
	switch {
	case netinfo.IsTunnelIface(inet):
		ui.Println(ui.Warn("весь трафик в интернет идёт в туннель " + inet))
	case len(tunnels) > 0:
		ui.Println(ui.Info("раздельный туннель: интернет идёт через " + ui.Dash(inet)))
	default:
		ui.Println(ui.Dim("интернет напрямую через " + ui.Dash(inet)))
	}
	if dns := netinfo.DNSServers(); len(dns) > 0 {
		ui.Println(ui.Label("dns ") + strings.Join(dns, " "))
	}
	if ok, total := BypassCount(cfg); total > 0 {
		line := ui.Label("обход ") + strconv.Itoa(ok) + " из " + strconv.Itoa(total) + " адресов"
		if ok < total {
			line += "  " + ui.Dim("tim vpn fix")
		}
		ui.Println(line)
		ui.Println(GuardLine())
	}
	if hasV6 {
		ui.Println(ui.Dim("обход работает только для IPv4; IPv6 через туннель остаётся как есть"))
	}
}

// BypassCount: сколько адресов целей сейчас идут мимо VPN и сколько их всего.
func BypassCount(cfg *config.Config) (ok, total int) {
	phys, gw := netinfo.Physical()
	for _, h := range cfg.Hosts {
		for _, d := range ExpandAddr(h.Addr) {
			total++
			if _, bad := RouteState(d, phys, gw); !bad && Bypassed(d, phys, gw) {
				ok++
			}
		}
	}
	return ok, total
}
