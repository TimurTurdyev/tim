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

// Screen: сервер - полосы места, памяти и нагрузки, лампы одной строкой и находки по группам.
// Последнее известное состояние видно сразу, свежее приходит по ssh следом.
func Screen(env *app.Env, target string) tui.Screen {
	return tui.NewBoard(tui.BoardOpts{
		Crumbs:  []string{"Серверы", target},
		Loading: "захожу по ssh на " + target + ", только чтение",
		Refresh: true,
		Command: func(*tui.BoardRow) string { return "tim server " + target },
		Load: func(fresh bool) tui.BoardData {
			if !fresh {
				if s, _, ok := saved(target); ok {
					return serverData(s)
				}
			}
			return serverData(collect(Server{Alias: target}))
		},
		Actions: []tui.BoardAction{
			{Key: "enter", Label: "отчёт", Free: true, Do: func(*tui.BoardRow) tui.Action { return env.Result([]string{"server", target}) }},
			{Key: "s", Label: "ssh", Free: true, Do: func(*tui.BoardRow) tui.Action {
				return tui.Action{Exit: func() { env.After([]string{"ssh", target}) }}
			}},
			{Key: "a", Label: "кто ломится", Free: true, Do: func(*tui.BoardRow) tui.Action {
				return tui.Action{Push: AttacksScreen(target, 24*time.Hour)}
			}},
			{Key: "t", Label: "трафик", Free: true, Do: func(*tui.BoardRow) tui.Action {
				return tui.Action{Push: TrafficScreen(target, 24*time.Hour)}
			}},
		},
	})
}

func serverData(s serverState) tui.BoardData {
	var d tui.BoardData
	if s.Err != "" {
		d.Status = "\u2715 не достучался"
		d.Empty = "Не достучался до " + s.Target + ": " + s.Err
		if old, oldAt, ok := saved(s.Target); ok {
			d.Empty += "; последнее известное: " + old.Report.OS + " \u00b7 проверялся " + ago(oldAt)
		}
		return d
	}
	r := s.Report
	d.Status = osLine(r)
	d.Head = serverBars(r)
	if text, lvl, ok := forecast(s.Target); ok && lvl == level.OK {
		d.Head = append(d.Head, ui.Label(ui.Pad("прогноз", 12))+text)
	}
	d.Head = append(d.Head, "", strings.Join(serverLamps(r), "   "))
	d.Columns = ui.Pad("ЧТО", 21) + "НАХОДКА"
	d.Wide = true
	groups := map[level.Level]string{level.Bad: "ВНИМАНИЕ", level.Warn: "ВНИМАНИЕ", level.OK: "БЕЗ ПОСЛЕДСТВИЙ", level.Skip: "НЕ ПРОВЕРЕНО"}
	counts := map[string]int{}
	for _, c := range s.Findings {
		counts[groups[c.Level]]++
	}
	for _, c := range s.Findings {
		g := groups[c.Level]
		row := findingRow(c)
		row.Group = g + " \u00b7 " + strconv.Itoa(counts[g])
		d.Rows = append(d.Rows, row)
	}
	d.Empty = "Замечаний нет"
	return d
}

// serverBars: диск, память и нагрузка полосами.
func serverBars(r srvinfo.Report) []string {
	bar := func(label string, pct int, tail string) string {
		return ui.Label(ui.Pad(label, 12)) + ui.Bar(pct, 20) + ui.Dim("  "+tail)
	}
	var out []string
	if disk, ok := r.Root(); ok {
		out = append(out, bar("диск", disk.Pct, ui.Bytes(disk.Used)+" из "+ui.Bytes(disk.Size)))
	}
	if r.MemTotal > 0 {
		used := r.MemTotal - r.MemAvail
		tail := ui.Bytes(used) + " из " + ui.Bytes(r.MemTotal)
		if r.SwapTotal > 0 {
			tail += " \u00b7 swap " + ui.Bytes(r.SwapTotal-r.SwapFree)
		}
		out = append(out, bar("память", int(used*100/r.MemTotal), tail))
	}
	if r.Cores > 0 {
		pct := min(100, int(r.Load[0]*100/float64(r.Cores)))
		out = append(out, bar("нагрузка", pct, fmt.Sprintf("%.2f %.2f %.2f за 1, 5, 15 мин на %d %s",
			r.Load[0], r.Load[1], r.Load[2], r.Cores, ui.Plural(r.Cores, "ядре", "ядрах", "ядрах"))))
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

// findingRow: находка строкой; что делать - раскрывается под ней на месте.
func findingRow(c health.Check) tui.BoardRow {
	what := c.Name
	if c.Text != "" && c.Level >= level.Warn {
		what += ", " + c.Text
	}
	row := tui.BoardRow{ID: c.Name + " " + c.Text,
		Cells: ui.Pad(level.Lamp(c.Level, ui.Clip(what, 18)), 21) + c.Detail}
	if c.Fix != "" {
		row.Inline = "что делать: " + c.Fix
	}
	return row
}
