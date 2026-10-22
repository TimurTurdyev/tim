package motd

import (
	"fmt"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/TimurTurdyev/tim/internal/bypass"
	"github.com/TimurTurdyev/tim/internal/config"
	"github.com/TimurTurdyev/tim/internal/gitx"
	"github.com/TimurTurdyev/tim/internal/machine"
	"github.com/TimurTurdyev/tim/internal/netinfo"
	"github.com/TimurTurdyev/tim/internal/par"
	"github.com/TimurTurdyev/tim/internal/procs"
	"github.com/TimurTurdyev/tim/internal/run"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/sshx"
	"github.com/TimurTurdyev/tim/internal/sysinfo"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// Карточка машины: метка в labelW колонок, память и диск полосами, остальное словами; цифры полосы
// занимают cellW, дальше вторая колонка. Полосы краснеют по порогам находок, а не по 60%: занятые
// две трети диска - это норма, а не повод для жёлтого.
const (
	labelW = 9
	barW   = 15
	cellW  = 36
)

func gb(b uint64) string { return fmt.Sprintf("%.0f", float64(b)/(1<<30)) }

func row(label, text string) string { return ui.Label(ui.Pad(label, labelW)) + text }

func system(*config.Config) []string {
	var (
		loadOK, diskOK, battOK bool
		loadPct                int
		memUsed, memTotal      uint64
		disk                   sysinfo.Disk
		batt                   sysinfo.Battery
	)
	par.All(
		func() { loadPct, loadOK = sysinfo.LoadPercent() },
		func() { memUsed, memTotal = sysinfo.Memory() },
		func() { disk, diskOK = sysinfo.DataDisk() },
		func() { batt, battOK = sysinfo.BatteryNow() },
	)
	var out []string
	if memTotal > 0 {
		cell := ui.BarAt(int(memUsed*100/memTotal), barW, 85, 95) + ui.Dim(gb(memUsed)+" из "+gb(memTotal)+" GB")
		var tail []string
		if loadOK {
			tail = append(tail, ui.Dim("нагрузка ")+ui.Percent(loadPct))
		}
		if battOK {
			tail = append(tail, ui.Dim("батарея ")+batteryText(batt))
		}
		out = append(out, row("память", ui.Pad(cell, cellW)+strings.Join(tail, "  ")))
	}
	if diskOK {
		out = append(out, row("диск", ui.BarAt(disk.Percent(), barW, 85, 95)+ui.Dim("свободно "+ui.Bytes(int64(disk.Avail)))))
	}
	// Пожиратели CPU - сюда же: про них и надо узнать при открытии терминала, а не через три дня.
	return append(out, machine.HogLines("")...)
}

func batteryText(b sysinfo.Battery) string {
	text := strconv.Itoa(b.Percent) + "%"
	if note := b.Note(); note != "" {
		text += ", " + note
	}
	if b.Low() {
		return ui.Warn(text)
	}
	return text
}

func project(cfg *config.Config) []string {
	cwd, _ := os.Getwd()
	p, ok := cfg.ProjectHere(cwd)
	if !ok {
		return nil
	}
	dir := cfg.ProjectDir(p)
	line := p.Name + ui.Dim("  "+run.Stack(dir))
	if g, ok := gitx.Status(dir); ok {
		line += "  " + g.Line()
	}
	port := run.ProjectPort(p, dir)
	if l, busy := netinfo.PortBusy(port); busy {
		line += "  " + ui.Info("порт "+port+" занят "+l.Command)
	}
	return []string{row("проект", line)}
}

// network: интерфейс и адрес; VPN и обход, если включён; живые туннели. Шлюз покажет tim net.
func network(cfg *config.Config) []string {
	phys, _ := netinfo.Physical()
	if phys.Device == "" {
		return []string{row("сеть", ui.Bad("активного интерфейса нет"))}
	}
	name := phys.Name
	if name != phys.Device {
		name += ui.Dim(" " + phys.Device)
	}
	parts := []string{name}
	if ip := netinfo.IfaceIPv4(phys.Device); ip != "" {
		parts = append(parts, ip)
	}
	inet := netinfo.InternetIface()
	if netinfo.IsTunnelIface(inet) {
		parts = append(parts, ui.Warn("VPN "+inet))
		if len(cfg.Hosts) > 0 {
			if ok, total, known := bypass.QuickCount(); known {
				state := fmt.Sprintf("обход %d/%d", ok, total)
				if ok < total {
					state = ui.Warn(state + " (tim route fix)")
				}
				parts = append(parts, state)
			}
		}
	}
	var tun []string
	for _, t := range sshx.Known() {
		if t.Alive() {
			tun = append(tun, t.Name)
		}
	}
	if _, ok := sshx.ShuttlePID(); ok {
		tun = append(tun, "all")
	}
	if len(tun) > 0 {
		parts = append(parts, ui.Info("туннели: "+strings.Join(tun, " ")))
	}
	return []string{row("сеть", strings.Join(parts, "  "))}
}

func ports(cfg *config.Config) []string {
	byProject := map[string][]string{}
	var names []string
	for _, r := range machine.Ports(cfg) {
		if r.Project == "" || r.Guessed {
			continue
		}
		if _, ok := byProject[r.Project]; !ok {
			names = append(names, r.Project)
		}
		item := ":" + strings.Join(r.Ports, ", :")
		if r.Kind != "" {
			item = r.Kind + " " + item
		}
		// Вторые сутки на порту - скорее всего забыли: tim ports --old.
		if r.Uptime >= machine.ForgottenAfter {
			item += ui.Warn(fmt.Sprintf(" (%d ч)", int(r.Uptime.Hours())))
		}
		byProject[r.Project] = append(byProject[r.Project], item)
	}
	if len(names) == 0 {
		return nil
	}
	sort.Strings(names)
	items := make([]string, len(names))
	for i, n := range names {
		items[i] = n + ": " + ui.Dim(strings.Join(byProject[n], ", "))
	}
	return []string{row("запущено", strings.Join(items, "  "))}
}

// services: службы brew и упавшие; контейнеры docker - тут же, это те же базы и очереди.
func services(*config.Config) []string {
	var up, bad []string
	var ok bool
	var containers map[int]sysinfo.Container
	par.All(
		func() { up, bad, ok = sysinfo.Services() },
		func() { containers = sysinfo.Containers() },
	)
	var parts []string
	if ok && len(up) > 0 {
		parts = append(parts, strings.Join(up, "  "))
	}
	if len(bad) > 0 {
		parts = append(parts, ui.Bad("упали: "+strings.Join(bad, ", "))+ui.Dim("  (tim svc)"))
	}
	if len(containers) > 0 {
		var names []string
		for _, c := range containers {
			if !slices.Contains(names, c.Name) {
				names = append(names, c.Name)
			}
		}
		sort.Strings(names)
		parts = append(parts, ui.Dim("docker: ")+strings.Join(names, ", "))
	}
	if len(parts) == 0 {
		return nil
	}
	return []string{row("службы", strings.Join(parts, "  "))}
}

func repos(cfg *config.Config) []string {
	type repoRow struct {
		name string
		g    gitx.State
	}
	var clones []*config.Project
	for i := range cfg.Projects {
		if gitx.IsRepo(cfg.ProjectDir(&cfg.Projects[i])) {
			clones = append(clones, &cfg.Projects[i])
		}
	}
	states := par.Map(clones, par.Limit, func(p *config.Project) repoRow {
		g, _ := gitx.Status(cfg.ProjectDir(p))
		return repoRow{p.Name, g}
	})
	var rows []repoRow
	for _, r := range states {
		if r.g.Changes > 0 || r.g.Ahead > 0 {
			rows = append(rows, r)
		}
	}
	if len(rows) == 0 {
		return []string{row("репо", ui.Dim("всё закоммичено и отправлено"))}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].name < rows[j].name })
	out := []string{row("репо", strconv.Itoa(len(rows))+ui.Dim(" с изменениями или неотправленным"))}
	for i, r := range rows {
		if i == 6 {
			out = append(out, ui.Dim(fmt.Sprintf("         и ещё %d", len(rows)-6)))
			break
		}
		out = append(out, "         "+ui.Pad(r.name, 22)+" "+r.g.Line())
	}
	return out
}

func hogs(*config.Config) []string {
	list, err := procs.All()
	if err != nil {
		return nil
	}
	sort.SliceStable(list, func(i, j int) bool { return list[i].CPU > list[j].CPU })
	var items []string
	for _, p := range list[:min(3, len(list))] {
		items = append(items, fmt.Sprintf("%s %.0f%%", procs.Name(p.Comm, p.Args), p.CPU))
	}
	return []string{row("cpu", strings.Join(items, "  "))}
}

func versions(*config.Config) []string {
	type probe struct {
		name string
		args []string
	}
	probes := []probe{
		{"go", []string{"go", "env", "GOVERSION"}},
		{"php", []string{"php", "-r", "echo PHP_VERSION;"}},
		{"node", []string{"node", "--version"}},
		{"python", []string{"python3", "-c", "import platform;print(platform.python_version())"}},
	}
	vals := par.Map(probes, len(probes), func(p probe) string {
		if !runner.Has(p.args[0]) {
			return ""
		}
		out, err := runner.Output(p.args[0], p.args[1:]...)
		if err != nil {
			return ""
		}
		return p.name + " " + strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(out), "go"), "v")
	})
	var items []string
	for _, v := range vals {
		if v != "" {
			items = append(items, v)
		}
	}
	return []string{row("версии", strings.Join(items, "  "))}
}
