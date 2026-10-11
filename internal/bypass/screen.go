package bypass

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/TimurTurdyev/tim/internal/app"
	"github.com/TimurTurdyev/tim/internal/config"
	"github.com/TimurTurdyev/tim/internal/netinfo"
	"github.com/TimurTurdyev/tim/internal/tui"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// Screen: VPN и обход - туннель, страж и состояние каждой цели.
func Screen(env *app.Env) tui.Screen {
	probe := false
	return tui.NewBoard(tui.BoardOpts{
		Crumbs:  []string{"Сеть и VPN"},
		Command: func(*tui.BoardRow) string { return "tim vpn" },
		Loading: "смотрю туннели и маршруты...",
		Load:    func(bool) tui.BoardData { return screenData(env.Cfg, probe) },
		Actions: []tui.BoardAction{
			{Key: "f", Label: "починить", Free: true, Do: func(*tui.BoardRow) tui.Action {
				return tui.Action{Exit: func() { env.After([]string{"route", "fix"}) }}
			}},
			{Key: "t", Label: "проверить порты", Free: true, Run: func(*tui.BoardRow) (string, error) {
				probe = true
				return "порт 22 проверен у каждой цели", nil
			}},
			{Key: "d", Label: "убрать цель", Do: func(row *tui.BoardRow) tui.Action {
				if row == nil {
					return tui.Action{}
				}
				return tui.Action{Exit: func() { env.After([]string{"direct", "rm", row.ID}) }}
			}},
			{Key: "s", Label: "страж", Free: true, Do: func(*tui.BoardRow) tui.Action { return env.Result([]string{"route", "status"}) }},
		},
	})
}

// target: строка цели и сколько её адресов ушли в туннель; ячейки собираются, когда известны ширины.
type target struct {
	row   tui.BoardRow
	bad   int
	count int
	state string // пусто - всё мимо VPN
}

func screenData(cfg *config.Config, probe bool) tui.BoardData {
	var d tui.BoardData
	phys, gw := netinfo.Physical()
	d.Status = phys.Name + " " + phys.Device + " \u00b7 шлюз " + ui.Dash(gw)
	d.Head = append(d.Head, vpnLamp(), guardLamp())
	if len(cfg.Hosts) == 0 {
		d.Head = append(d.Head, ui.Dim("целей обхода нет: tim direct <сайт|адрес|подсеть|as:N>"))
		d.Empty = "цели обхода появятся здесь"
		return d
	}
	var targets []target
	total, ok := 0, 0
	expanded := ExpandHosts(cfg.Hosts)
	for i, h := range cfg.Hosts {
		t, dests, fine := targetRow(h, expanded[i], phys, gw, probe)
		total += dests
		ok += fine
		targets = append(targets, t)
	}
	sort.SliceStable(targets, func(i, j int) bool { return targets[i].bad > targets[j].bad })
	summary := ui.OK(fmt.Sprintf("\u25cf обход %d из %d ", ok, total)) + ui.Dim("адресов идут мимо VPN")
	if ok < total {
		summary += "   " + ui.Warn(fmt.Sprintf("\u25b2 %d через туннель", total-ok)) + ui.Dim("  f - починить")
	}
	d.Head = append(d.Head, summary)
	addrW, labelW, anyBad := len("ЦЕЛЬ"), len("ПОМЕТКА"), false
	for _, t := range targets {
		addrW = max(addrW, ui.Width(t.row.ID))
		labelW = max(labelW, ui.Width(t.row.Sub))
		anyBad = anyBad || t.state != ""
	}
	addrW, labelW = min(addrW, 28), min(labelW, 24)
	// Состояние - только у отклонений: когда всё мимо VPN, столбец одинаковых строк не нужен.
	head := ui.Pad("ЦЕЛЬ", addrW+2) + ui.Pad("ПОМЕТКА", labelW+2) + "АДРЕСОВ"
	if anyBad {
		head = ui.Pad(head, addrW+labelW+4+9) + "СОСТОЯНИЕ"
	}
	d.Columns = ui.Dim(head)
	for _, t := range targets {
		t.row.Cells = ui.Fit(t.row.ID, addrW) + "  " + ui.Fit(ui.Dim(t.row.Sub), labelW) + "  " + ui.Pad(strconv.Itoa(t.count), 9) + t.state
		d.Rows = append(d.Rows, t.row)
	}
	return d
}

// targetRow: строка цели с адресами в панели; dests - сколько адресов, fine - сколько идут мимо VPN.
func targetRow(h config.Host, addrs []string, phys netinfo.Service, gw string, probe bool) (t target, dests, fine int) {
	t.row = tui.BoardRow{ID: h.Addr, Title: h.Addr, Sub: h.Label}
	if len(addrs) == 0 {
		t.bad = 1
		t.row.Pane = []string{ui.Bad("не раскрывается в адреса: домен не резолвится или AS без сетей")}
	}
	for _, dest := range addrs {
		state, bad := RouteState(dest, phys, gw)
		if !bad && Bypassed(dest, phys, gw) {
			fine++
		}
		if bad {
			t.bad++
		}
		line := ui.Pad(dest, 20) + state
		if !bad && gw != "" {
			line += ui.Dim(" через шлюз " + gw + " (" + phys.Device + ")")
		}
		if probe && !strings.Contains(dest, "/") {
			if PortOpen(dest, "22") {
				line += ui.Dim("  порт 22 отвечает")
			} else {
				line += ui.Bad("  порт 22 молчит")
			}
		}
		if len(t.row.Pane) < 8 {
			t.row.Pane = append(t.row.Pane, line)
		}
	}
	if rest := len(addrs) - 8; rest > 0 {
		t.row.Pane = append(t.row.Pane, ui.Dim(fmt.Sprintf("и ещё %d", rest)))
	}
	if t.bad > 0 {
		t.row.Pane = append(t.row.Pane, ui.Dim("\u2192 f снимет протухшие маршруты и заведёт все цели заново, нужен sudo"))
	}
	t.count = len(addrs)
	switch {
	case len(addrs) == 0:
		t.state = ui.Bad("\u2715 нет адресов")
	case t.bad > 0:
		t.state = ui.Warn("\u25b2 " + ui.Count(t.bad, "адрес", "адреса", "адресов") + " через туннель")
	}
	return t, len(addrs), fine
}

func vpnLamp() string {
	tunnels := netinfo.Tunnels()
	inet := netinfo.InternetIface()
	switch {
	case netinfo.IsTunnelIface(inet):
		return ui.OK("\u25cf VPN включён ") + ui.Dim(inet+", весь трафик в туннель")
	case len(tunnels) > 0:
		return ui.OK("\u25cf VPN включён ") + ui.Dim("раздельный туннель, интернет через "+ui.Dash(inet))
	}
	return ui.Dim("\u25cb VPN выключен, интернет напрямую через " + ui.Dash(inet))
}

func guardLamp() string {
	st, hasSt := GuardStatus()
	switch {
	case !GuardInstalled():
		return ui.Warn("\u25b2 страж не установлен ") + ui.Dim("обход сам не починится: sudo tim route install")
	case !GuardRunning():
		return ui.Bad("\u2715 страж не работает ") + ui.Dim("tim route status")
	case !hasSt:
		return ui.OK("\u25cf страж работает ") + ui.Dim("отчёта пока нет")
	case time.Since(st.LastCheck) > time.Minute:
		return ui.Warn("\u25b2 страж молчит "+ui.Duration(time.Since(st.LastCheck))) + ui.Dim("  tim route status")
	}
	return ui.OK("\u25cf страж работает ") + ui.Dim("проверка "+ui.Duration(time.Since(st.LastCheck))+" назад")
}
