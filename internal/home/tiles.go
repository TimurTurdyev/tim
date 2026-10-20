package home

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/TimurTurdyev/tim/internal/bypass"
	"github.com/TimurTurdyev/tim/internal/gitx"
	"github.com/TimurTurdyev/tim/internal/health"
	"github.com/TimurTurdyev/tim/internal/machine"
	"github.com/TimurTurdyev/tim/internal/netinfo"
	"github.com/TimurTurdyev/tim/internal/run"
	"github.com/TimurTurdyev/tim/internal/sysinfo"
	"github.com/TimurTurdyev/tim/internal/tui"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// Плитки главного экрана: состояние машины, обхода VPN, проекта и служб. Данные о машине считает
// sysinfo, беды - health: те же, что в сводке tim status.

// tiles: плитки по порядку важности и признаки беды для предупреждения и маскота.
func (h *Home) tiles() ([]tui.TileData, health.Machine) {
	var tiles []tui.TileData
	var m health.Machine
	if data, ok := sysinfo.DataDisk(); ok {
		pct := data.Percent()
		m.DiskPct, m.DiskFree = pct, int64(data.Avail)
		tiles = append(tiles, tui.TileData{Title: "диск", Width: 22, Rows: []string{
			tui.Bar(float64(pct)/100, 10) + " " + ui.Percent(pct),
			ui.Dim("свободно " + ui.Bytes(int64(data.Avail))),
		}})
	}
	if used, total := sysinfo.Memory(); total > 0 {
		pct := int(used * 100 / total)
		tiles = append(tiles, tui.TileData{Title: "память", Width: 22, Rows: []string{
			tui.Bar(float64(pct)/100, 10) + " " + ui.Percent(pct),
			ui.Dim(fmt.Sprintf("%.0f из %.0f GB", float64(used)/(1<<30), float64(total)/(1<<30))),
		}})
	}
	m.SwapShort = machine.SwapShort()
	tiles = append(tiles, machineTile())
	if len(h.env.Cfg.Hosts) > 0 {
		m.BypassOK, m.BypassAll = bypass.BypassCount(h.env.Cfg)
		m.GuardMissing = !bypass.GuardAlive()
		tiles = append(tiles, vpnTile(m))
	}
	if t, ok := h.projectTile(); ok {
		tiles = append(tiles, t)
	}
	tiles = append(tiles, serversTile())
	if t, ok := servicesTile(); ok {
		tiles = append(tiles, t)
	}
	return tiles, m
}

// troubleScreen: куда вести по enter из предупреждения.
func (h *Home) troubleScreen(kind health.Trouble) func() tui.Action {
	return func() tui.Action {
		switch kind {
		case health.DiskFull:
			return tui.Action{Push: diskScreen(h.env, nil)}
		case health.Swap:
			return tui.Action{Push: machine.HWScreen()}
		}
		return tui.Action{Push: bypass.Screen(h.env)}
	}
}

// machineTile: нагрузка в долях ядер и батарея; без батареи - сколько работает.
func machineTile() tui.TileData {
	rows := []string{ui.Dim("нагрузка -"), ""}
	if pct, ok := sysinfo.LoadPercent(); ok {
		rows[0] = "нагрузка " + ui.Percent(pct)
	}
	if b, ok := sysinfo.BatteryNow(); ok {
		text := "батарея " + strconv.Itoa(b.Percent) + "%"
		if note := b.Note(); note != "" {
			text += ", " + note
		}
		rows[1] = ui.Dim(text)
		if b.Low() {
			rows[1] = ui.Warn(text)
		}
	} else if boot, ok := sysinfo.Boot(); ok {
		rows[1] = ui.Dim("работает " + ui.Duration(time.Since(boot)))
	}
	return tui.TileData{Title: "машина", Width: 26, Rows: rows}
}

func vpnTile(m health.Machine) tui.TileData {
	guard := ui.Dim("страж работает")
	if m.GuardMissing {
		guard = ui.Dim("страж не установлен")
	}
	lamp := ui.OK(fmt.Sprintf("\u25cf обход %d/%d", m.BypassOK, m.BypassAll))
	if m.BypassOK < m.BypassAll {
		lamp = ui.Warn(fmt.Sprintf("\u25b2 обход %d/%d", m.BypassOK, m.BypassAll))
	}
	return tui.TileData{Title: "vpn", Width: 24, Rows: []string{lamp, guard}}
}

func serversTile() tui.TileData {
	var names []string
	for _, s := range run.ListStates() {
		if s.Alive() {
			names = append(names, s.Name+" :"+s.Port)
		}
	}
	rows := []string{ui.Dim("\u25cb нет запущенных"), ui.Dim("tim serve <проект> -d")}
	if len(names) > 0 {
		rows = []string{ui.OK("\u25cf " + ui.Plural(len(names), "работает", "работают", "работают")), ui.Dim(strings.Join(names, ", "))}
	}
	return tui.TileData{Title: "dev-серверы", Width: 26, Rows: rows}
}

// projectTile: проект, в папке которого открыт терминал, - ветка, изменения и порт.
func (h *Home) projectTile() (tui.TileData, bool) {
	cwd, _ := os.Getwd()
	p, ok := h.env.Cfg.ProjectHere(cwd)
	if !ok {
		return tui.TileData{}, false
	}
	dir := h.env.Cfg.ProjectDir(p)
	rows := []string{p.Name, ""}
	if g, ok := gitx.Status(dir); ok {
		rows[0] += ui.Dim("  " + g.Line())
	}
	if port := run.ProjectPort(p, dir); port != "" {
		if l, busy := netinfo.PortBusy(port); busy {
			rows[1] = ui.OK("\u25cf порт " + port + " занят " + l.Command)
		} else {
			rows[1] = ui.Dim("порт " + port + " свободен")
		}
	}
	return tui.TileData{Title: "проект", Width: 30, Rows: rows}, true
}

// servicesTile: службы - сколько работает и что упало.
func servicesTile() (tui.TileData, bool) {
	up, bad, ok := sysinfo.Services()
	if !ok || len(up) == 0 && len(bad) == 0 {
		return tui.TileData{}, false
	}
	rows := []string{ui.OK("\u25cf " + ui.Count(len(up), "служба", "службы", "служб")), ui.Dim(fitNames(up, 26))}
	if len(bad) > 0 {
		rows = []string{ui.Bad("\u2715 упали: " + fitNames(bad, 26)), ui.Dim(fitNames(up, 26))}
	}
	return tui.TileData{Title: "службы", Width: 26, Rows: rows}, true
}

// fitNames: имена через запятую, сколько влезает в width, остальные - "+N".
func fitNames(names []string, width int) string {
	out := ""
	for i, n := range names {
		next := n
		if out != "" {
			next = out + ", " + n
		}
		if ui.Width(next)+4 > width && i > 0 {
			return out + fmt.Sprintf(" +%d", len(names)-i)
		}
		out = next
	}
	return out
}
