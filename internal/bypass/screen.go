package bypass

import (
	"fmt"
	"slices"
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

var groupOrder = []string{"СЕРВЕРЫ", "САЙТЫ", "СЕТИ AS", "ПОДСЕТИ"}

// Screen: Сеть - туннель, страж и цели обхода группами; n добавляет цель строкой ввода внизу.
func Screen(env *app.Env) tui.Screen {
	probe := false
	return tui.NewBoard(tui.BoardOpts{
		Crumbs:  []string{"Сеть"},
		Command: func(*tui.BoardRow) string { return "tim vpn" },
		Loading: "смотрю туннели и маршруты",
		Load:    func(bool) tui.BoardData { return screenData(env.Cfg, probe) },
		Enter: func(*tui.BoardRow) string {
			return "адреса цели и их маршруты из таблицы"
		},
		Actions: []tui.BoardAction{
			{Key: "enter", Label: "адреса", Do: func(row *tui.BoardRow) tui.Action {
				lines := row.Card.Extra
				return tui.Action{Push: tui.NewResult("адреса "+row.ID, func() (string, error) {
					return strings.Join(lines, "\n"), nil
				}, tui.ResultOpts{})}
			}},
			{Key: "n", Label: "добавить", Free: true, Do: func(*tui.BoardRow) tui.Action {
				return tui.Action{Ask: &tui.Ask{
					Placeholder: "сайт, адрес или as:номер - пойдёт мимо VPN",
					Hint:        "enter - добавить (sudo, tim закроется)",
					Done: func(text string) tui.Action {
						return tui.Action{Exit: func() { env.After([]string{"direct", text}) }}
					},
				}}
			}},
			{Key: "f", Label: "починить", Note: "sudo, закроет tim", Free: true, Do: func(*tui.BoardRow) tui.Action {
				return tui.Action{Exit: func() { env.After([]string{"route", "fix"}) }}
			}},
			{Key: "c", Label: "проверить", Doing: "проверяю порт 22 у целей", Free: true, Hidden: true, Run: func(*tui.BoardRow) (string, error) {
				probe = true
				return "порты целей: проверены, ответ в карточке", nil
			}},
			{Key: "x", Label: "убрать", Note: "sudo, закроет tim", Do: func(row *tui.BoardRow) tui.Action {
				return tui.Action{Exit: func() { env.After([]string{"direct", "rm", row.ID}) }}
			}},
			{Key: "l", Label: "журнал", Free: true, Hidden: true, Do: func(*tui.BoardRow) tui.Action { return env.Result([]string{"route", "status"}) }},
		},
	})
}

// target: строка цели и сколько её адресов ушли в туннель.
type target struct {
	row   tui.BoardRow
	group string
	bad   int
	count int
	state string // пусто - всё мимо VPN
}

// groupOf: серверы - адреса, сайты - имена, сети AS и подсети - отдельно.
func groupOf(addr string) string {
	switch {
	case strings.HasPrefix(addr, "as:"):
		return "СЕТИ AS"
	case strings.Contains(addr, "/"):
		return "ПОДСЕТИ"
	case netinfo.IsIPv4(addr):
		return "СЕРВЕРЫ"
	}
	return "САЙТЫ"
}

func screenData(cfg *config.Config, probe bool) tui.BoardData {
	var d tui.BoardData
	phys, gw := netinfo.Physical()
	if len(cfg.Hosts) == 0 {
		d.Head = append(d.Head, netLine(phys, gw, ""))
		d.Status = "0 целей"
		d.Empty = "Целей обхода нет: добавленный сайт, адрес или as:номер пойдёт мимо VPN"
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
	d.Status = ui.Count(len(cfg.Hosts), "цель", "цели", "целей") + fmt.Sprintf(" \u00b7 %d из %d мимо VPN", ok, total)
	d.Head = append(d.Head, netLine(phys, gw, guardWord()))
	if ok < total {
		d.Head = append(d.Head, ui.Warn(fmt.Sprintf("\u25b2 обход %d из %d: %s через туннель", ok, total, ui.Count(total-ok, "адрес идёт", "адреса идут", "адресов идут")))+
			ui.Dim(" \u00b7 починка - sudo, tim закроется"))
	}
	if g := guardTrouble(); g != "" {
		d.Head = append(d.Head, g)
	}
	counts := map[string]int{}
	for i := range targets {
		targets[i].group = groupOf(targets[i].row.ID)
		counts[targets[i].group]++
	}
	sort.SliceStable(targets, func(i, j int) bool {
		return slices.Index(groupOrder, targets[i].group) < slices.Index(groupOrder, targets[j].group)
	})
	addrW, labelW := len("ЦЕЛЬ"), len("ПОМЕТКА")
	for _, t := range targets {
		addrW = max(addrW, ui.Width(t.row.ID))
		labelW = max(labelW, ui.Width(t.row.Sub))
	}
	addrW, labelW = min(addrW, 20), min(labelW, 22)
	d.Columns = ui.Pad("ЦЕЛЬ", addrW+2) + ui.Pad("ПОМЕТКА", labelW+2) + "АДРЕСОВ"
	for _, t := range targets {
		t.row.Group = t.group + " \u00b7 " + strconv.Itoa(counts[t.group])
		t.row.Find = t.row.Sub
		count := fmt.Sprintf("%7d", t.count)
		switch {
		case t.count == 0:
			count = ui.Bad(fmt.Sprintf("%7s", "\u2715 0"))
		case t.bad > 0:
			// Часть адресов ушла в туннель: число янтарное с лампой, подробности - в карточке.
			count = ui.Warn(fmt.Sprintf("%7s", "\u25b2 "+strconv.Itoa(t.count)))
		}
		t.row.Cells = ui.Fit(t.row.ID, addrW) + "  " + ui.Fit(ui.Dim(t.row.Sub), labelW) + "  " + count
		d.Rows = append(d.Rows, t.row)
	}
	return d
}

// netLine: туннель, шлюз и страж одной строкой, когда всё в порядке.
func netLine(phys netinfo.Service, gw, guard string) string {
	parts := []string{vpnLamp()}
	if gw != "" {
		parts = append(parts, ui.Dim("шлюз "+gw+" ("+phys.Name+" "+phys.Device+")"))
	}
	if guard != "" {
		parts = append(parts, ui.Dim(guard))
	}
	return strings.Join(parts, ui.Dim(" \u00b7 "))
}

// targetRow: строка цели с адресами в карточке; dests - сколько адресов, fine - сколько идут мимо VPN.
func targetRow(h config.Host, addrs []string, phys netinfo.Service, gw string, probe bool) (t target, dests, fine int) {
	card := &tui.Card{Title: h.Addr, Sub: h.Label}
	t.row = tui.BoardRow{ID: h.Addr, Sub: h.Label, Card: card}
	if len(addrs) == 0 {
		t.bad = 1
		card.Facts = append(card.Facts, tui.Fact{Label: ui.Bad("\u2715 адресов"), Value: "нет: домен не резолвится или у AS нет сетей"})
	}
	var lines []string
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
		lines = append(lines, line)
	}
	card.Extra = lines
	switch {
	case len(addrs) == 1:
		card.Extra = nil
		card.Facts = append(card.Facts, tui.Fact{Label: "маршрут", Value: strings.TrimSpace(strings.TrimPrefix(lines[0], addrs[0]))})
	case len(addrs) > 1:
		card.Facts = append(card.Facts, tui.Fact{Label: "адресов", Value: strconv.Itoa(len(addrs)) + ui.Dim(fmt.Sprintf(" \u00b7 мимо VPN %d", fine))})
		card.Extra = append([]string{}, lines[:min(6, len(lines))]...)
		if len(lines) > 6 {
			card.Extra = append(card.Extra, ui.Dim(fmt.Sprintf("и ещё %d", len(lines)-6)))
		}
	}
	t.count = len(addrs)
	switch {
	case len(addrs) == 0:
		t.state = ui.Bad("\u2715 нет адресов")
	case t.bad > 0:
		t.state = ui.Warn("\u25b2 " + ui.Count(t.bad, "через туннель", "через туннель", "через туннель"))
	}
	return t, len(addrs), fine
}

func vpnLamp() string {
	tunnels := netinfo.Tunnels()
	inet := netinfo.InternetIface()
	switch {
	case netinfo.IsTunnelIface(inet):
		return ui.OK("\u25cf VPN "+inet) + ", весь трафик в туннель"
	case len(tunnels) > 0:
		return ui.OK("\u25cf VPN") + ", раздельный туннель, интернет через " + ui.Dash(inet)
	}
	return ui.Dim("\u25cb VPN выключен") + ", интернет напрямую через " + ui.Dash(inet)
}

// guardWord: страж в строке ламп, когда он в порядке, - давность его проверки.
func guardWord() string {
	st, ok := GuardStatus()
	if !GuardInstalled() || !GuardRunning() || !ok || time.Since(st.LastCheck) > time.Minute {
		return ""
	}
	return "страж: " + ui.Short(time.Since(st.LastCheck))
}

// guardTrouble: страж - отдельной строкой, если с ним беда.
func guardTrouble() string {
	st, hasSt := GuardStatus()
	switch {
	case !GuardInstalled():
		return ui.Warn("\u25b2 страж: не установлен") + ui.Dim(" \u00b7 обход сам не починится: sudo tim route install")
	case !GuardRunning():
		return ui.Bad("\u2715 страж: не работает") + ui.Dim(" \u00b7 журнал - в справке")
	case hasSt && time.Since(st.LastCheck) > time.Minute:
		return ui.Warn("\u25b2 страж: молчит "+ui.Short(time.Since(st.LastCheck))) + ui.Dim(" \u00b7 журнал - в справке")
	}
	return ""
}
