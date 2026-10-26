package servers

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/TimurTurdyev/tim/internal/app"
	"github.com/TimurTurdyev/tim/internal/baseline"
	"github.com/TimurTurdyev/tim/internal/health"
	"github.com/TimurTurdyev/tim/internal/level"
	"github.com/TimurTurdyev/tim/internal/srvinfo"
	"github.com/TimurTurdyev/tim/internal/tui"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// newPrefix: у строки нового ID начинается с него - по нему space отмечает своим.
const newPrefix = "new "

// Screen: сервер - полосы места, памяти и нагрузки, лампы одной строкой и находки по группам.
// Последнее известное состояние видно сразу, свежее приходит по ssh следом.
func Screen(env *app.Env, target string) tui.Screen {
	return tui.NewBoard(tui.BoardOpts{
		Crumbs:  []string{"Серверы", target},
		Loading: "захожу по ssh на " + target + ", только чтение",
		Refresh: true,
		Command: func(*tui.BoardRow) string { return "tim server " + target },
		Palette: func() (string, []tui.CommandEntry) { return recipePalette(env, target) },
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
			{Key: "c", Label: "проверить", Free: true, Doing: "глубокая проверка " + target + ": файлы сайтов и системы, около минуты",
				Run: func(*tui.BoardRow) (string, error) {
					d, err := runDeep(target)
					if err != nil {
						return "", err
					}
					return "глубокая проверка: " + srvinfo.EvaluateDeep(d)[0].Detail, nil
				}},
			{Key: "s", Label: "ssh", Free: true, Do: func(*tui.BoardRow) tui.Action {
				return tui.Action{Exit: func() { env.After([]string{"ssh", target}) }}
			}},
			{Key: "a", Label: "кто ломится", Free: true, Do: func(*tui.BoardRow) tui.Action {
				return tui.Action{Push: AttacksScreen(env, target, 24*time.Hour)}
			}},
			{Key: "t", Label: "трафик", Free: true, Do: func(*tui.BoardRow) tui.Action {
				return tui.Action{Push: TrafficScreen(env, target, 24*time.Hour)}
			}},
			{Key: "space", Label: "отметить своим", Doing: "отмечаю своим",
				When: func(row *tui.BoardRow) bool { return strings.HasPrefix(row.ID, newPrefix) },
				Run: func(row *tui.BoardRow) (string, error) {
					kind, key, _ := strings.Cut(strings.TrimPrefix(row.ID, newPrefix), " ")
					return "отмечено своим, больше не новое", baseline.Accept(object(target), kind, key)
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
	fresh := baseline.Pending(object(s.Target))
	if len(fresh) > 0 {
		d.Status += " \u00b7 " + strconv.Itoa(len(fresh)) + " " + ui.Plural(len(fresh), "новое", "новых", "новых")
	}
	d.Head = serverBars(r)
	if text, lvl, ok := forecast(s.Target); ok && lvl == level.OK {
		d.Head = append(d.Head, ui.Label(ui.Pad("прогноз", 12))+text)
	}
	lamps := serverLamps(r)
	if age := deepAge(s.Target); age != "" {
		lamps = append(lamps, ui.Dim(age))
	}
	d.Head = append(d.Head, "", strings.Join(lamps, "   "))
	if ports := portsLine(r, s.Findings); ports != "" {
		d.Head = append(d.Head, ports)
	}
	if s.Remembered != "" && len(fresh) == 0 {
		d.Head = append(d.Head, ui.Dim(s.Remembered))
	}
	d.Columns = ui.Pad("ЧТО", 25) + "НАХОДКА"
	d.Wide = true
	group := "НОВОЕ С " + baseline.Since(fresh).Local().Format("02.01 15:04") + " \u00b7 " + strconv.Itoa(len(fresh))
	for _, it := range fresh {
		d.Rows = append(d.Rows, tui.BoardRow{ID: newPrefix + it.Kind + " " + it.Key, Group: group,
			Cells:  ui.Pad(level.Lamp(level.Warn, ui.Clip(newWhat(it), 22)), 25) + newText(it),
			Inline: "что делать: " + newFix(s.Target, it)})
	}
	groups := map[level.Level]string{level.Bad: "ВНИМАНИЕ", level.Warn: "ВНИМАНИЕ", level.OK: "БЕЗ ПОСЛЕДСТВИЙ",
		level.Info: "БЕЗ ПОСЛЕДСТВИЙ", level.Skip: "НЕ ПРОВЕРЕНО"}
	all := append(append([]health.Check{}, s.Findings...), deepFindings(s.Target)...)
	slices.SortStableFunc(all, func(a, b health.Check) int { return int(b.Level) - int(a.Level) })
	counts := map[string]int{}
	for _, c := range all {
		counts[groups[c.Level]]++
	}
	for _, c := range all {
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
	if srvinfo.KeyOnly(r) {
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
	return lamps
}

// portsLine: порты наружу одной строкой; те, о которых есть находка, - янтарные и в конце, чтобы лампа
// не спорила с находками ниже.
func portsLine(r srvinfo.Report, findings []health.Check) string {
	risky := map[string]bool{}
	for _, c := range findings {
		if c.Name == "порт" && c.Level >= level.Warn {
			for _, f := range strings.Fields(c.Text) {
				if _, err := strconv.Atoi(f); err == nil {
					risky[f] = true
				}
			}
		}
	}
	var open []int
	for _, l := range r.Listening {
		if l.Public {
			open = append(open, l.Port)
		}
	}
	if len(open) == 0 {
		return ""
	}
	slices.Sort(open)
	var calm, warn []string
	for _, p := range slices.Compact(open) {
		if ps := strconv.Itoa(p); risky[ps] {
			warn = append(warn, ui.Warn("\u25b2 "+ps))
		} else {
			calm = append(calm, ps)
		}
	}
	line := level.Lamp(level.OK, "наружу ") + strings.Join(calm, " ")
	if len(warn) > 0 {
		line += "   " + strings.Join(warn, "   ")
	}
	return line
}

// findingRow: находка строкой; что делать - раскрывается под ней на месте.
func findingRow(c health.Check) tui.BoardRow {
	// В колонке ЧТО - предмет: порт с номером, остальное - именем находки.
	what := c.Name
	if c.Name == "порт" {
		what = "порт " + c.Text
		if nums := strings.Fields(c.Text); len(nums) > 1 && isNumber(nums[1]) {
			what = "порты " + strings.Join(nums, ", ")
		}
	}
	row := tui.BoardRow{ID: c.Name + " " + c.Text,
		Cells: ui.Pad(level.Lamp(c.Level, ui.Clip(what, 22)), 25) + c.Detail}
	if c.Fix != "" {
		row.Inline = "что делать: " + c.Fix
	}
	return row
}

func isNumber(s string) bool {
	_, err := strconv.Atoi(s)
	return err == nil
}
