package cmd

import (
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/TimurTurdyev/tim/internal/procs"
	"github.com/TimurTurdyev/tim/internal/sysinfo"
	"github.com/TimurTurdyev/tim/internal/tui"
)

// Экраны машины: живое железо и процессы по приложениям.

func hwTitle(s hwSnapshot) string {
	return s.model + " \u00b7 " + strconv.Itoa(s.cores) + " ядер \u00b7 " + fmtBytes(s.memTotal)
}

// hwScreen: паспорт собирается при первом замере, уже в фоне - открытие экрана не ждёт system_profiler.
func hwScreen() tui.Screen {
	collect, sample, err := hwFuncs()
	if err != nil {
		return tui.NewResult("hw", func() (string, error) { return "", err }, tui.ResultOpts{})
	}
	var once sync.Once
	var base hwSnapshot
	return tui.NewHW(tui.HWOpts{
		Sample: func() tui.HWSample {
			once.Do(func() { base = collect() })
			s := sample(base)
			if d, ok := sysinfo.DataDisk(); ok {
				s.Disk = d.Percent()
			}
			return s
		},
		Procs: func() tui.Screen { return procsScreen("") },
	})
}

func procsScreen(query string) tui.Screen {
	return tui.NewProcs(tui.ProcsOpts{Query: query, Load: loadProcs, Close: closeGroup})
}

func loadProcs() tui.ProcsData {
	var data tui.ProcsData
	list, err := allProcs()
	if err != nil {
		return data
	}
	var sum float64
	for _, p := range list {
		sum += p.CPU
	}
	data.CPU = int(sum/float64(runtime.NumCPU()) + 0.5)
	used, total := sysinfo.Memory()
	data.MemUsed, data.MemTotal = int64(used), int64(total)
	for _, a := range procs.Groups(list) {
		data.Groups = append(data.Groups, procGroup(a))
	}
	return data
}

func procGroup(a procs.App) tui.ProcGroup {
	g := tui.ProcGroup{Name: a.Name, CPU: a.CPU, RSS: a.RSS, Age: a.Main.Uptime, IsApp: a.IsApp}
	sorted := append([]procs.Full(nil), a.Procs...)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].CPU*1e6+float64(sorted[i].RSS) > sorted[j].CPU*1e6+float64(sorted[j].RSS)
	})
	for _, p := range sorted {
		if procs.Protected(p) {
			g.Protected = true
		}
		g.Procs = append(g.Procs, tui.ProcRow{PID: p.PID, Name: rowName(p), Note: stateNote(p.State), CPU: p.CPU, RSS: p.RSS})
	}
	return g
}

// stateNote: как stateHint, но без цвета - экран красит сам.
func stateNote(s string) string {
	switch {
	case strings.HasPrefix(s, "Z"):
		return "зомби: уже завершился, ждёт родителя"
	case strings.HasPrefix(s, "U"), strings.HasPrefix(s, "D"):
		return "ждёт диск или сеть и не прерывается"
	case strings.HasPrefix(s, "T"):
		return "остановлен"
	}
	return ""
}

// closeGroup: процессы перечитываются, чтобы не слать сигналы по устаревшим pid.
func closeGroup(g tui.ProcGroup, force bool) (string, error) {
	list, err := allProcs()
	if err != nil {
		return "", err
	}
	for _, a := range procs.Groups(list) {
		if a.Name == g.Name {
			return closeApp(a, force)
		}
	}
	return g.Name + " уже закрыт", nil
}
