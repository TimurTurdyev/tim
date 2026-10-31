package motd

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/TimurTurdyev/tim/internal/bypass"
	"github.com/TimurTurdyev/tim/internal/config"
	"github.com/TimurTurdyev/tim/internal/health"
	"github.com/TimurTurdyev/tim/internal/machine"
	"github.com/TimurTurdyev/tim/internal/netinfo"
	"github.com/TimurTurdyev/tim/internal/par"
	"github.com/TimurTurdyev/tim/internal/procs"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/sshx"
	"github.com/TimurTurdyev/tim/internal/sysinfo"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// Карточка машины: подпись Teal, память и диск полосами, остальное словами. Полосы краснеют по
// порогам находок, а не по 60%: занятые две трети диска - это норма, а не повод для жёлтого.
const barW = 15

func gb(b uint64) string { return fmt.Sprintf("%.0f", float64(b)/(1<<30)) }

// tail: расшифровка после полосы - через два пробела, части через точку.
func tail(parts ...string) string { return "  " + strings.Join(parts, ui.Dim(" \u00b7 ")) }

func system(*config.Config) []Line {
	var (
		loadOK, diskOK    bool
		loadPct           int
		memUsed, memTotal uint64
		disk              sysinfo.Disk
		h                 health.Machine
	)
	par.All(
		func() { loadPct, loadOK = sysinfo.LoadPercent() },
		func() { memUsed, memTotal = sysinfo.Memory() },
		func() { disk, diskOK = sysinfo.DataDisk() },
		func() {
			h.MemFree, _ = sysinfo.MemFree()
			if sw, ok := machine.SwapNow(); ok {
				h.SwapUsed, h.SwapTotal = sw.Used, sw.Total
			}
		},
	)
	var out []Line
	if memTotal > 0 {
		parts := []string{ui.Dim(gb(memUsed) + " из " + gb(memTotal) + " GB")}
		switch {
		case h.SwapShort():
			parts = append(parts, ui.Warn("swap "+ui.Bytes(h.SwapUsed)))
		case loadOK:
			parts = append(parts, ui.Dim("нагрузка ")+ui.Percent(loadPct))
		}
		out = append(out, Line{"память", ui.Bar(int(memUsed*100/memTotal), barW) + tail(parts...), ToProcs})
	}
	if diskOK {
		out = append(out, Line{"диск", ui.Bar(disk.Percent(), barW) + tail(ui.Dim("свободно "+ui.Bytes(int64(disk.Avail)))), ToDisk})
	}
	return out
}

// network: интерфейс и адрес; VPN и обход, если включён; живые туннели. Шлюз покажет tim net.
func network(cfg *config.Config) []Line {
	phys, _ := netinfo.Physical()
	if phys.Device == "" {
		return []Line{{"сеть", ui.Bad("\u2715 активного интерфейса нет"), ToNet}}
	}
	name := phys.Name
	if name != phys.Device {
		name += ui.Dim(" " + phys.Device)
	}
	if ip := netinfo.IfaceIPv4(phys.Device); ip != "" {
		name += "  " + ip
	}
	parts := []string{name}
	inet := netinfo.InternetIface()
	if netinfo.IsTunnelIface(inet) {
		parts = append(parts, ui.Warn("VPN "+inet))
		if len(cfg.Hosts) > 0 {
			if ok, total, known := bypass.QuickCount(); known {
				state := fmt.Sprintf("обход %d из %d", ok, total)
				if ok < total {
					state = ui.Warn(state)
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
	return []Line{{"сеть", strings.Join(parts, ui.Dim(" \u00b7 ")), ToNet}}
}

// ports: что запущено в папках работы - по папке, с портами; давнее - янтарным.
func ports(cfg *config.Config) []Line {
	byFolder := map[string][]string{}
	var names []string
	for _, r := range machine.Ports(cfg) {
		if r.Folder == "" {
			continue
		}
		if _, ok := byFolder[r.Folder]; !ok {
			names = append(names, r.Folder)
		}
		item := ":" + strings.Join(r.Ports, ", :")
		if r.Kind != "" {
			item = r.Kind + " " + item
		}
		// Давно на порту - скорее всего забыли: tim ports --old.
		if r.Uptime >= machine.ForgottenAfter {
			item += ui.Warn(" " + ui.Short(r.Uptime))
		}
		byFolder[r.Folder] = append(byFolder[r.Folder], item)
	}
	if len(names) == 0 {
		return nil
	}
	sort.Strings(names)
	items := make([]string, len(names))
	for i, n := range names {
		items[i] = n + "  " + ui.Dim(strings.Join(byFolder[n], ", "))
	}
	return []Line{{"запущено", strings.Join(items, ui.Dim(" \u00b7 ")), ToRunning}}
}

// services: службы brew и упавшие; контейнеры docker - тут же, это те же базы и очереди.
func services(*config.Config) []Line {
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
		parts = append(parts, ui.Bad("упали: "+strings.Join(bad, ", "))+ui.Dim("  (brew services list)"))
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
	return []Line{{Label: "службы", Value: strings.Join(parts, "  ")}}
}

func hogs(*config.Config) []Line {
	list, err := procs.All()
	if err != nil {
		return nil
	}
	sort.SliceStable(list, func(i, j int) bool { return list[i].CPU > list[j].CPU })
	var items []string
	for _, p := range list[:min(3, len(list))] {
		items = append(items, fmt.Sprintf("%s %.0f%%", procs.Name(p.Comm, p.Args), p.CPU))
	}
	return []Line{{Label: "cpu", Value: strings.Join(items, "  ")}}
}

func versions(*config.Config) []Line {
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
	return []Line{{Label: "версии", Value: strings.Join(items, "  ")}}
}
