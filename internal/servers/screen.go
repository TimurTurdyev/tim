package servers

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/TimurTurdyev/tim/internal/app"
	"github.com/TimurTurdyev/tim/internal/health"
	"github.com/TimurTurdyev/tim/internal/level"
	"github.com/TimurTurdyev/tim/internal/srvinfo"
	"github.com/TimurTurdyev/tim/internal/tui"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// Screen: отчёт о сервере - полосы места, памяти и нагрузки, лампы и находки с подсказкой, что делать.
func Screen(env *app.Env, target string) tui.Screen {
	return tui.NewBoard(tui.BoardOpts{
		Crumbs:  []string{"Сервер", target},
		Loading: "захожу по ssh на " + target + ", только чтение",
		Command: func(*tui.BoardRow) string { return "tim server " + target },
		Load:    func(bool) tui.BoardData { return serverData(target) },
		Actions: []tui.BoardAction{
			{Key: "s", Label: "ssh", Free: true, Do: func(*tui.BoardRow) tui.Action {
				return tui.Action{Exit: func() { env.After([]string{"ssh", target}) }}
			}},
			{Key: "enter", Label: "отчёт целиком", Free: true, Do: func(*tui.BoardRow) tui.Action { return env.Result([]string{"server", target}) }},
			{Key: "a", Label: "кто ломится", Free: true, Do: func(*tui.BoardRow) tui.Action {
				return tui.Action{Push: AttacksScreen(target, 24*time.Hour)}
			}},
		},
	})
}

func serverData(target string) tui.BoardData {
	var d tui.BoardData
	r, err := srvinfo.Collect(target, serverTimeout)
	if err != nil {
		d.Status = "не достучался"
		d.Head = []string{level.Lamp(level.Bad, target+": "+sshError(err)), ui.Dim("маршрут и вход проверит tim ssh " + target + "; s - зайти по ssh")}
		d.Empty = "отчёта нет"
		return d
	}
	d.Status = osLine(r)
	d.Head = serverBars(r)
	if text, lvl, ok := forecast(target); ok && lvl == level.OK {
		d.Head = append(d.Head, ui.Pad("прогноз", 10)+text)
	}
	d.Lamps = serverLamps(r)
	for _, c := range evaluate(target, r) {
		d.Rows = append(d.Rows, findingRow(c))
	}
	d.Empty = "замечаний нет"
	return d
}

// serverBars: диск, память и нагрузка полосами.
func serverBars(r srvinfo.Report) []string {
	bar := func(label string, pct int, tail string) string {
		return ui.Pad(label, 10) + tui.Bar(float64(pct)/100, 30) + "  " + ui.Percent(pct) + ui.Dim("  "+tail)
	}
	var out []string
	if disk, ok := r.Root(); ok {
		out = append(out, bar("диск", disk.Pct, ui.Bytes(disk.Used)+" из "+ui.Bytes(disk.Size)))
	}
	if r.MemTotal > 0 {
		used := r.MemTotal - r.MemAvail
		tail := ui.Bytes(used) + " из " + ui.Bytes(r.MemTotal)
		if r.SwapTotal > 0 {
			tail += ", swap " + ui.Bytes(r.SwapTotal-r.SwapFree)
		}
		out = append(out, bar("память", int(used*100/r.MemTotal), tail))
	}
	if r.Cores > 0 {
		pct := int(r.Load[1] * 100 / float64(r.Cores))
		out = append(out, bar("нагрузка", pct, fmt.Sprintf("%.2f за 5 мин, %d %s", r.Load[1], r.Cores, ui.Plural(r.Cores, "ядро", "ядра", "ядер"))))
	}
	return out
}

// serverLamps: что проверено и в порядке, и порты наружу; беды - строками ниже, без повтора в лампах.
func serverLamps(r srvinfo.Report) []string {
	var lamps []string
	if !slices.ContainsFunc(r.Failed, func(u string) bool { return srvinfo.Harmless(u, r) == "" }) {
		lamps = append(lamps, level.Lamp(level.OK, "службы в порядке"))
	}
	if len(r.SecurityUpdates) == 0 && !r.Reboot && !r.NoApt {
		lamps = append(lamps, level.Lamp(level.OK, "обновления безопасности стоят"))
	}
	if !r.NoSudoSSHD && !srvinfo.PasswordLogin(r) {
		lamps = append(lamps, level.Lamp(level.OK, "ssh только по ключу"))
	}
	if r.Fail2ban {
		lamps = append(lamps, level.Lamp(level.OK, "fail2ban работает"))
	}
	if r.CrowdSec {
		lamps = append(lamps, level.Lamp(level.OK, "CrowdSec работает"))
	}
	if r.UFW == "active" {
		lamps = append(lamps, level.Lamp(level.OK, "ufw включён"))
	}
	var open []int
	for _, l := range r.Listening {
		if l.Public {
			open = append(open, l.Port)
		}
	}
	if len(open) > 0 {
		slices.Sort(open)
		ports := make([]string, len(open))
		for i, p := range open {
			ports[i] = strconv.Itoa(p)
		}
		lamps = append(lamps, level.Lamp(level.OK, "наружу ")+ui.Dim(strings.Join(slices.Compact(ports), ", ")))
	}
	return lamps
}

// findingRow: находка строкой доски; в панели подробность и с чего начать.
func findingRow(c health.Check) tui.BoardRow {
	row := tui.BoardRow{ID: c.Name + " " + c.Text, Title: c.Name + ": " + c.Text}
	if c.Level > level.OK {
		row.Sub = c.Level.String()
	}
	row.Cells = ui.Pad(level.Lamp(c.Level, ui.Pad(c.Name, 12)), 16) + c.Detail
	row.Pane = []string{c.Detail}
	if c.Fix != "" {
		row.Pane = append(row.Pane, ui.Dim("\u2192 "+c.Fix))
	}
	return row
}
