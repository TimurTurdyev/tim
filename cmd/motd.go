package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"

	"github.com/TimurTurdyev/tim/internal/netinfo"
	"github.com/TimurTurdyev/tim/internal/registry"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/sshx"
	"github.com/TimurTurdyev/tim/internal/sysinfo"
	"github.com/TimurTurdyev/tim/internal/ui"
)

type section struct {
	name    string
	timeout time.Duration
	collect func() []string
}

var sections = []section{
	{"project", 800 * time.Millisecond, motdProject},
	{"net", 800 * time.Millisecond, motdNet},
	{"ports", 800 * time.Millisecond, motdPorts},
	{"services", 800 * time.Millisecond, motdServices},
	{"repos", 2500 * time.Millisecond, motdRepos},
	{"procs", 800 * time.Millisecond, motdProcs},
	{"versions", 800 * time.Millisecond, motdVersions},
}

var defaultMotd = []string{"project", "net", "ports", "services"}

var (
	motdAll  bool
	motdTime bool
)

var motdCmd = &cobra.Command{
	Use:       "motd [раздел...] [--all] [--time]",
	Short:     "Сводка: проект, сеть, порты, сервисы, репозитории, процессы, версии",
	Long:      "Разделы: project, net, ports, services, repos, procs, versions. По умолчанию ключ motd в конфиге или project net ports services.",
	ValidArgs: []string{"project", "net", "ports", "services", "repos", "procs", "versions"},
	RunE: func(cmd *cobra.Command, args []string) error {
		want := args
		switch {
		case motdAll:
			want = nil
			for _, s := range sections {
				want = append(want, s.name)
			}
		case len(want) == 0 && len(cfg.Motd) > 0:
			want = cfg.Motd
		case len(want) == 0:
			want = defaultMotd
		}
		chosen := map[string]bool{}
		for _, w := range want {
			found := false
			for _, s := range sections {
				found = found || s.name == w
			}
			if !found {
				return ui.Errorf("нет раздела %s: project, net, ports, services, repos, procs, versions", w)
			}
			chosen[w] = true
		}
		start := time.Now()
		type result struct {
			lines []string
			took  time.Duration
			late  bool
		}
		results := make([]result, len(sections))
		var wg sync.WaitGroup
		for i, s := range sections {
			if !chosen[s.name] {
				continue
			}
			wg.Go(func() {
				t0 := time.Now()
				done := make(chan []string, 1)
				go func() { done <- s.collect() }()
				select {
				case lines := <-done:
					results[i] = result{lines: lines, took: time.Since(t0)}
				case <-time.After(s.timeout):
					results[i] = result{took: s.timeout, late: true}
				}
			})
		}
		wg.Wait()
		for i, s := range sections {
			if !chosen[s.name] {
				continue
			}
			r := results[i]
			for _, l := range r.lines {
				ui.Println(l)
			}
			if motdTime {
				note := fmt.Sprintf("%s %d мс", s.name, r.took.Milliseconds())
				if r.late {
					note += ", не успел"
				}
				ui.Println(ui.Dim("  " + note))
			}
		}
		if motdTime {
			ui.Println(ui.Dim(fmt.Sprintf("всего %d мс", time.Since(start).Milliseconds())))
		}
		return nil
	},
}

func gitState(dir string) (sysinfo.GitState, bool) {
	out, err := runner.Output("git", "-C", dir, "status", "-sb", "--porcelain")
	if err != nil {
		return sysinfo.GitState{}, false
	}
	return sysinfo.ParseGitStatusSB(out), true
}

func gitLine(g sysinfo.GitState) string {
	parts := []string{g.Branch}
	if g.Changes > 0 {
		parts = append(parts, ui.Warn(strconv.Itoa(g.Changes)+" изм."))
	}
	if g.Ahead > 0 {
		parts = append(parts, ui.Info("+"+strconv.Itoa(g.Ahead)+" не отправлено"))
	}
	if g.Behind > 0 {
		parts = append(parts, ui.Warn("-"+strconv.Itoa(g.Behind)+" отстаёт"))
	}
	return strings.Join(parts, "  ")
}

func motdProject() []string {
	cwd, _ := os.Getwd()
	p, ok := cfg.ProjectHere(cwd)
	if !ok {
		return nil
	}
	dir := cfg.ProjectDir(p)
	line := ui.Label("проект   ") + p.Name + ui.Dim("  "+registry.Stack(dir))
	if g, ok := gitState(dir); ok {
		line += "  " + gitLine(g)
	}
	port := projectPort(p, dir)
	if l, busy := netinfo.PortBusy(port); busy {
		line += "  " + ui.Info("порт "+port+" занят "+l.Command)
	}
	return []string{line}
}

func motdNet() []string {
	phys, gw := physical()
	if phys.Device == "" {
		return []string{ui.Bad("сеть     ") + "активного интерфейса нет"}
	}
	line := ui.Label("сеть     ") + phys.Name + ui.Dim(" "+phys.Device+" шлюз "+gw)
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
	if _, ok := shuttlePID(); ok {
		tun = append(tun, "all")
	}
	if len(tun) > 0 {
		line += "  " + ui.Info("туннели: "+strings.Join(tun, " "))
	}
	if len(cfg.Hosts) > 0 && netinfo.IsTunnelIface(inet) {
		total, ok := 0, 0
		for _, h := range cfg.Hosts {
			for _, d := range expandAddr(h.Addr) {
				total++
				if bypassed(d, phys, gw) {
					ok++
				}
			}
		}
		state := fmt.Sprintf("обход %d/%d", ok, total)
		if ok < total {
			state = ui.Warn(state + " (tim vpn fix)")
		}
		line += "  " + state
	}
	return []string{line}
}

func motdPorts() []string {
	byProject := map[string][]string{}
	var names []string
	for _, r := range listenRows() {
		if r.project == "" || strings.HasSuffix(r.project, "?") {
			continue
		}
		if _, ok := byProject[r.project]; !ok {
			names = append(names, r.project)
		}
		byProject[r.project] = append(byProject[r.project], r.Port)
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

func motdServices() []string {
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

func motdRepos() []string {
	type row struct {
		name string
		g    sysinfo.GitState
	}
	var mu sync.Mutex
	var rows []row
	sem := make(chan struct{}, 8)
	var wg sync.WaitGroup
	for i := range cfg.Projects {
		p := &cfg.Projects[i]
		dir := cfg.ProjectDir(p)
		if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
			continue
		}
		wg.Go(func() {
			sem <- struct{}{}
			defer func() { <-sem }()
			if g, ok := gitState(dir); ok && (g.Changes > 0 || g.Ahead > 0) {
				mu.Lock()
				rows = append(rows, row{p.Name, g})
				mu.Unlock()
			}
		})
	}
	wg.Wait()
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
		out = append(out, "         "+ui.Pad(r.name, 22)+" "+gitLine(r.g))
	}
	return out
}

func motdProcs() []string {
	out, err := runner.Output("ps", "-Ao", "pcpu=,comm=", "-r")
	if err != nil {
		return nil
	}
	var items []string
	for _, p := range sysinfo.ParsePsTop(out, 3) {
		items = append(items, fmt.Sprintf("%s %.0f%%", p.Name, p.CPU))
	}
	return []string{ui.Label("cpu      ") + strings.Join(items, "  ")}
}

func motdVersions() []string {
	probes := []struct {
		name string
		args []string
	}{
		{"go", []string{"go", "env", "GOVERSION"}},
		{"php", []string{"php", "-r", "echo PHP_VERSION;"}},
		{"node", []string{"node", "--version"}},
		{"python", []string{"python3", "-c", "import platform;print(platform.python_version())"}},
	}
	vals := make([]string, len(probes))
	var wg sync.WaitGroup
	for i, p := range probes {
		wg.Go(func() {
			if !runner.Has(p.args[0]) {
				return
			}
			out, err := runner.Output(p.args[0], p.args[1:]...)
			if err == nil {
				vals[i] = p.name + " " + strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(out), "go"), "v")
			}
		})
	}
	wg.Wait()
	var items []string
	for _, v := range vals {
		if v != "" {
			items = append(items, v)
		}
	}
	return []string{ui.Label("версии   ") + strings.Join(items, "  ")}
}

func init() {
	motdCmd.Flags().BoolVar(&motdAll, "all", false, "все разделы")
	motdCmd.Flags().BoolVar(&motdTime, "time", false, "сколько занял каждый раздел")
	root.AddCommand(motdCmd)
}
