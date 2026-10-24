package cmd

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/TimurTurdyev/tim/internal/bypass"
	"github.com/TimurTurdyev/tim/internal/netinfo"
	"github.com/TimurTurdyev/tim/internal/tui"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// ---------- VPN и обход ----------

func vpnScreen() tui.Screen {
	probe := false
	return tui.NewBoard(tui.BoardOpts{
		Crumbs:  []string{"Сеть", "VPN и обход"},
		Command: func(*tui.BoardRow) string { return "tim vpn" },
		Loading: "смотрю туннели и маршруты...",
		Load:    func(bool) tui.BoardData { return vpnData(probe) },
		Actions: []tui.BoardAction{
			{Key: "f", Label: "починить", Do: func(*tui.BoardRow) tui.Action {
				return tui.Action{Exit: func() { runAfter([]string{"vpn", "fix"}) }}
			}},
			{Key: "t", Label: "проверить порты", Run: func(*tui.BoardRow) (string, error) {
				probe = true
				return "порт 22 проверен у каждой цели", nil
			}},
			{Key: "d", Label: "убрать цель", Do: func(row *tui.BoardRow) tui.Action {
				if row == nil {
					return tui.Action{}
				}
				return tui.Action{Exit: func() { runAfter([]string{"route", "rm", row.ID}) }}
			}},
			{Key: "s", Label: "страж", Do: func(*tui.BoardRow) tui.Action { return resultAction([]string{"route", "status"}) }},
		},
	})
}

func vpnData(probe bool) tui.BoardData {
	var d tui.BoardData
	phys, gw := bypass.Physical()
	d.Status = phys.Name + " " + phys.Device + " \u00b7 шлюз " + ui.Dash(gw)
	tunnels := netinfo.Tunnels()
	inet := netinfo.InternetIface()
	var vpn string
	switch {
	case netinfo.IsTunnelIface(inet):
		vpn = ui.OK("\u25cf VPN включён ") + ui.Dim(inet+", весь трафик в туннель")
	case len(tunnels) > 0:
		vpn = ui.OK("\u25cf VPN включён ") + ui.Dim("раздельный туннель, интернет через "+ui.Dash(inet))
	default:
		vpn = ui.Dim("\u25cb VPN выключен, интернет напрямую через " + ui.Dash(inet))
	}
	var guard string
	st, hasSt := bypass.GuardStatus()
	switch {
	case !bypass.GuardInstalled():
		guard = ui.Warn("\u25b2 страж не установлен ") + ui.Dim("после смены сети обход сам не починится: sudo tim route install")
	case !bypass.GuardRunning():
		guard = ui.Bad("\u2715 страж не работает ") + ui.Dim("tim route status")
	case !hasSt:
		guard = ui.OK("\u25cf страж работает ") + ui.Dim("отчёта пока нет")
	case time.Since(st.LastCheck) > time.Minute:
		guard = ui.Warn("\u25b2 страж молчит "+ui.Duration(time.Since(st.LastCheck))) + ui.Dim("  tim route status")
	default:
		guard = ui.OK("\u25cf страж работает ") + ui.Dim("проверка "+ui.Duration(time.Since(st.LastCheck))+" назад")
	}
	d.Head = append(d.Head, ui.Pad(vpn, 60)+guard)
	if len(cfg.Hosts) == 0 {
		d.Head = append(d.Head, ui.Dim("целей обхода нет: tim route add <адрес|домен|подсеть|as:N>"))
		d.Empty = "цели обхода появятся здесь"
		return d
	}
	type target struct {
		row  tui.BoardRow
		bad  int
		dest int
	}
	var targets []target
	total, ok := 0, 0
	for _, h := range cfg.Hosts {
		dests := bypass.ExpandAddr(h.Addr)
		t := target{row: tui.BoardRow{ID: h.Addr, Title: h.Addr, Sub: h.Label}, dest: len(dests)}
		if len(dests) == 0 {
			t.bad = 1
			t.row.Pane = []string{ui.Bad("не раскрывается в адреса: домен не резолвится или AS без сетей")}
		}
		for _, dest := range dests {
			total++
			state, bad := bypass.RouteState(dest, phys, gw)
			if !bad && bypass.Bypassed(dest, phys, gw) {
				ok++
			}
			if bad {
				t.bad++
			}
			line := ui.Pad(dest, 20) + state
			if probe && !strings.Contains(dest, "/") {
				if bypass.PortOpen(dest, "22") {
					line += ui.Dim("  порт 22 отвечает")
				} else {
					line += ui.Bad("  порт 22 молчит")
				}
			}
			if len(t.row.Pane) < 8 {
				t.row.Pane = append(t.row.Pane, line)
			}
		}
		if rest := len(dests) - 8; rest > 0 {
			t.row.Pane = append(t.row.Pane, ui.Dim(fmt.Sprintf("и ещё %d", rest)))
		}
		if t.bad > 0 {
			t.row.Pane = append(t.row.Pane, ui.Dim("\u2192 f снимет протухшие маршруты и заведёт все цели заново, нужен sudo"))
		}
		state := ui.OK("\u25cf мимо VPN")
		switch {
		case len(dests) == 0:
			state = ui.Bad("\u2715 нет адресов")
		case t.bad > 0:
			state = ui.Warn(fmt.Sprintf("\u25b2 %d %s через туннель", t.bad, ui.Plural(t.bad, "адрес", "адреса", "адресов")))
		}
		t.row.Cells = ui.Pad(h.Addr, 26) + ui.Pad(ui.Dim(h.Label), 14) + ui.Pad(strconv.Itoa(len(dests)), 8) + state
		targets = append(targets, t)
	}
	sort.SliceStable(targets, func(i, j int) bool { return targets[i].bad > targets[j].bad })
	bypass := ui.OK(fmt.Sprintf("\u25cf обход %d из %d ", ok, total)) + ui.Dim("адресов идут мимо VPN")
	if ok < total {
		bypass += "   " + ui.Warn(fmt.Sprintf("\u25b2 %d через туннель", total-ok)) + ui.Dim("  f - починить")
	}
	d.Head = append(d.Head, bypass, "",
		ui.Dim(ui.Pad("ЦЕЛЬ", 26)+ui.Pad("ПОМЕТКА", 14)+ui.Pad("АДРЕСОВ", 8)+"СОСТОЯНИЕ"))
	for _, t := range targets {
		d.Rows = append(d.Rows, t.row)
	}
	return d
}
