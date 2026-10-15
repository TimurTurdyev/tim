package bypass

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/TimurTurdyev/tim/internal/config"
	"github.com/TimurTurdyev/tim/internal/netinfo"
	"github.com/TimurTurdyev/tim/internal/routewatch"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// printRoutes: интерфейс, куда идёт трафик, страж и состояние каждой цели; probe - проверить порт 22.
// routeRow: строка таблицы целей - цель и пометка только у первого адреса.
type routeRow struct {
	name, label, dest, state string
	bad                      bool
}

// printRoutes: куда идёт трафик и где каждая цель. Без all и probe - только адреса с отклонением
// и итог: 725 одинаковых строк "в обход" ничего не говорят.
func printRoutes(cfg *config.Config, probe, all bool) {
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
		ui.Println(ui.Dim("целей обхода нет: tim direct <сайт|адрес|подсеть|as:N>"))
		return
	}
	ui.Println(GuardLine())
	var rows []routeRow
	total, bad := 0, 0
	expanded := ExpandHosts(cfg.Hosts)
	for hi, h := range cfg.Hosts {
		dests := expanded[hi]
		if len(dests) == 0 {
			rows = append(rows, routeRow{name: h.Addr, label: h.Label, state: ui.Bad("не раскрывается в адреса"), bad: true})
			bad++
			continue
		}
		for i, d := range dests {
			name, label := h.Addr, h.Label
			if i > 0 {
				name, label = "", ""
			}
			state, wrong := RouteState(d, phys, gw)
			if probe && !strings.Contains(d, "/") {
				state += "  " + portNote(d)
			}
			total++
			if wrong {
				bad++
			}
			rows = append(rows, routeRow{name: h.Addr, label: label, dest: d, state: state, bad: wrong})
			rows[len(rows)-1].name = name
		}
	}
	full := all || probe
	nameW, labelW, destW := len("ЦЕЛЬ"), len("ПОМЕТКА"), len("АДРЕС")
	for _, r := range rows {
		if full || r.bad {
			nameW, labelW, destW = max(nameW, ui.Width(r.name)), max(labelW, ui.Width(r.label)), max(destW, ui.Width(r.dest))
		}
	}
	nameW, labelW, destW = min(nameW, 28), min(labelW, 24), min(destW, 18)
	if full || bad > 0 {
		ui.Println(ui.Dim(ui.Pad("ЦЕЛЬ", nameW) + "  " + ui.Pad("ПОМЕТКА", labelW) + "  " + ui.Pad("АДРЕС", destW) + "  СОСТОЯНИЕ"))
	}
	for _, r := range rows {
		if full || r.bad {
			ui.Println(ui.Fit(r.name, nameW) + "  " + ui.Fit(r.label, labelW) + "  " + ui.Fit(r.dest, destW) + "  " + r.state)
		}
	}
	if !full {
		line := fmt.Sprintf("в обход %d из %d адресов, целей %d", total-bad, total, len(cfg.Hosts))
		if bad == 0 {
			line = ui.OK(line)
		} else {
			line = ui.Warn(line)
		}
		ui.Println(line + ui.Dim("; весь список: tim route --all"))
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
	kinds := slices.Collect(maps.Keys(sum))
	slices.SortFunc(kinds, func(a, b routewatch.Kind) int {
		return cmp.Or(cmp.Compare(sum[b], sum[a]), cmp.Compare(a, b))
	})
	for _, k := range kinds {
		ui.Println(ui.Dim("  " + ui.Pad(strconv.Itoa(sum[k]), 5) + k.String()))
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
			line += "  " + ui.Dim("tim route fix")
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
	for _, dests := range ExpandHosts(cfg.Hosts) {
		for _, d := range dests {
			total++
			if _, bad := RouteState(d, phys, gw); !bad && Bypassed(d, phys, gw) {
				ok++
			}
		}
	}
	return ok, total
}
