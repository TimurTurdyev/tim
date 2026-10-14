package motd

import (
	"fmt"
	"os"
	osuser "os/user"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

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

func gb(b uint64) string { return fmt.Sprintf("%.0f", float64(b)/(1<<30)) }

func system(*config.Config) []string {
	var (
		osName, batt, ip, docker string
		boot                     time.Time
		booted                   bool
		load                     []float64
		memUsed, memTotal        uint64
		disk                     sysinfo.Disk
		diskOK                   bool
	)
	par.All(
		func() { osName = sysinfo.OS() },
		func() { boot, booted = sysinfo.Boot() },
		func() { load = sysinfo.Load() },
		func() { memUsed, memTotal = sysinfo.Memory() },
		func() { disk, diskOK = sysinfo.DataDisk() },
		func() { batt, _ = runner.Output("pmset", "-g", "batt") },
		func() {
			if phys, ok := netinfo.PhysIface(); ok {
				ip = netinfo.IfaceIPv4(phys.Device)
			}
		},
		func() { docker = dockerLine() },
	)
	user := os.Getenv("USER")
	if u, err := osuser.Current(); err == nil {
		user = u.Username
	}
	host, _ := os.Hostname()
	host, _, _ = strings.Cut(host, ".")
	line1 := ui.Label(user+"@"+host) + "  " + osName + "  " + time.Now().Format("Mon 02 Jan, 15:04")
	if booted {
		line1 += ui.Dim("  up " + ui.Duration(time.Since(boot)))
	}
	var parts []string
	if l := load; len(l) == 3 {
		parts = append(parts, ui.Label("load ")+fmt.Sprintf("%.2f %.2f %.2f", l[0], l[1], l[2]))
	}
	if memUsed > 0 && memTotal > 0 {
		parts = append(parts, ui.Label("mem ")+gb(memUsed)+"/"+gb(memTotal)+" GB")
	}
	if d, ok := disk, diskOK; ok {
		text := gb(d.Used) + "/" + gb(d.Size) + " GB (" + strconv.Itoa(d.Percent()) + "%)"
		if d.Percent() >= 90 {
			text = ui.Bad(text)
		}
		parts = append(parts, ui.Label("disk ")+text)
	}
	if b, ok := sysinfo.ParsePmset(batt); ok {
		text := strconv.Itoa(b.Percent) + "% " + b.State
		if b.Remaining != "" && b.Remaining != "0:00" {
			text += " " + b.Remaining
		}
		if b.Percent <= 20 && !b.AC {
			text = ui.Warn(text)
		}
		parts = append(parts, ui.Label("batt ")+text)
	}
	line2 := strings.Join(parts, "   ")
	if ip != "" {
		line2 += "   " + ui.Label("ip ") + ip
	}
	if docker != "" {
		line2 += "   " + ui.Dim(docker)
	}
	// Пожиратели CPU - сюда же: про них и надо узнать при открытии терминала, а не через три дня.
	return append([]string{line1, line2}, machine.HogLines("")...)
}

func dockerLine() string {
	if !runner.Has("docker") || !sysinfo.DockerUp() {
		return ""
	}
	out, err := runner.Output("docker", "ps", "-q")
	if err != nil {
		return "docker запущен"
	}
	return "docker " + strconv.Itoa(len(strings.Fields(out))) + " конт."
}

func project(cfg *config.Config) []string {
	cwd, _ := os.Getwd()
	p, ok := cfg.ProjectHere(cwd)
	if !ok {
		return nil
	}
	dir := cfg.ProjectDir(p)
	line := ui.Label("проект   ") + p.Name + ui.Dim("  "+run.Stack(dir))
	if g, ok := gitx.Status(dir); ok {
		line += "  " + g.Line()
	}
	port := run.ProjectPort(p, dir)
	if l, busy := netinfo.PortBusy(port); busy {
		line += "  " + ui.Info("порт "+port+" занят "+l.Command)
	}
	return []string{line}
}

func network(cfg *config.Config) []string {
	phys, gw := netinfo.Physical()
	if phys.Device == "" {
		return []string{ui.Bad("сеть     ") + "активного интерфейса нет"}
	}
	name := phys.Name
	if name != phys.Device {
		name += ui.Dim(" " + phys.Device)
	}
	line := ui.Label("сеть     ") + name + ui.Dim(" шлюз "+gw)
	inet := netinfo.InternetIface()
	if netinfo.IsTunnelIface(inet) {
		line += "  " + ui.Warn("интернет через "+inet)
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
		line += "  " + ui.Info("туннели: "+strings.Join(tun, " "))
	}
	if len(cfg.Hosts) > 0 && netinfo.IsTunnelIface(inet) {
		ok, total := bypass.BypassCount(cfg)
		seenBypass.Store(&[2]int{ok, total})
		state := fmt.Sprintf("обход %d/%d", ok, total)
		if ok < total {
			state = ui.Warn(state + " (tim route fix)")
		}
		line += "  " + state
	}
	return []string{line}
}

func ports(cfg *config.Config) []string {
	byProject := map[string][]string{}
	var names []string
	for _, r := range machine.Ports(cfg) {
		if r.Project == "" || strings.HasSuffix(r.Project, "?") {
			continue
		}
		if _, ok := byProject[r.Project]; !ok {
			names = append(names, r.Project)
		}
		byProject[r.Project] = append(byProject[r.Project], r.Port)
	}
	if len(names) == 0 {
		return nil
	}
	sort.Strings(names)
	items := make([]string, len(names))
	for i, n := range names {
		items[i] = n + " " + ui.Dim(strings.Join(byProject[n], ","))
	}
	return []string{ui.Label("запущено ") + strings.Join(items, "  ")}
}

func services(*config.Config) []string {
	if runtime.GOOS == "linux" {
		if failed := sysinfo.FailedUnits(); len(failed) > 0 {
			return []string{ui.Label("сервисы  ") + ui.Bad("упали: "+strings.Join(failed, " ")) + ui.Dim("  (tim svc)")}
		}
		return nil
	}
	out, err := runner.Output("launchctl", "list")
	if err != nil {
		return nil
	}
	var up, bad []string
	for _, a := range sysinfo.ParseLaunchctl(out) {
		switch {
		case a.Running():
			up = append(up, a.Name)
		case a.Status != 0:
			bad = append(bad, a.Name+" ("+strconv.Itoa(a.Status)+")")
		}
	}
	if len(up) == 0 && len(bad) == 0 {
		return nil
	}
	sort.Strings(up)
	line := ui.Label("сервисы  ") + strings.Join(up, " ")
	if len(bad) > 0 {
		line += "  " + ui.Bad("упали: "+strings.Join(bad, " "))
	}
	return []string{line}
}

func repos(cfg *config.Config) []string {
	type row struct {
		name string
		g    gitx.State
	}
	var clones []*config.Project
	for i := range cfg.Projects {
		if gitx.IsRepo(cfg.ProjectDir(&cfg.Projects[i])) {
			clones = append(clones, &cfg.Projects[i])
		}
	}
	states := par.Map(clones, par.Limit, func(p *config.Project) row {
		g, _ := gitx.Status(cfg.ProjectDir(p))
		return row{p.Name, g}
	})
	var rows []row
	for _, r := range states {
		if r.g.Changes > 0 || r.g.Ahead > 0 {
			rows = append(rows, r)
		}
	}
	if len(rows) == 0 {
		return []string{ui.Label("репо     ") + ui.Dim("всё закоммичено и отправлено")}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].name < rows[j].name })
	out := []string{ui.Label("репо     ") + strconv.Itoa(len(rows)) + ui.Dim(" с изменениями или неотправленным")}
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
	return []string{ui.Label("cpu      ") + strings.Join(items, "  ")}
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
	return []string{ui.Label("версии   ") + strings.Join(items, "  ")}
}
