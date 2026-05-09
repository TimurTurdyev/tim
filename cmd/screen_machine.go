package cmd

import (
	"runtime"
	"sort"
	"sync"

	"github.com/TimurTurdyev/tim/internal/machine"
	"github.com/TimurTurdyev/tim/internal/procs"
	"github.com/TimurTurdyev/tim/internal/sysinfo"
	"github.com/TimurTurdyev/tim/internal/tui"
)

// Экраны машины: живое железо и процессы по приложениям.

// hwScreen: паспорт собирается при первом замере, уже в фоне - открытие экрана не ждёт system_profiler.
func hwScreen() tui.Screen {
	collect, sample, err := machine.Funcs()
	if err != nil {
		return tui.NewResult("hw", func() (string, error) { return "", err }, tui.ResultOpts{})
	}
	var once sync.Once
	var base machine.Snapshot
	return tui.NewHW(tui.HWOpts{
		Sample: func() tui.HWSample {
			once.Do(func() { base = collect() })
			s := toHWSample(sample(base))
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
	list, err := machine.All()
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
		g.Procs = append(g.Procs, tui.ProcRow{PID: p.PID, Name: machine.RowName(p), Note: machine.StateNote(p.State), CPU: p.CPU, RSS: p.RSS})
	}
	return g
}

// closeGroup: процессы перечитываются, чтобы не слать сигналы по устаревшим pid.
func closeGroup(g tui.ProcGroup, force bool) (string, error) {
	list, err := machine.All()
	if err != nil {
		return "", err
	}
	for _, a := range procs.Groups(list) {
		if a.Name == g.Name {
			return machine.Close(a, force)
		}
	}
	return g.Name + " уже закрыт", nil
}

// toHWSample: замер модуля в тип экрана - поля те же, пакеты разные.
func toHWSample(l machine.Live) tui.HWSample {
	out := tui.HWSample{Title: l.Title, CPU: l.CPU, GPU: l.GPU, Mem: l.Mem, GPUValid: l.GPUValid, VRAM: l.VRAM,
		MemUsed: l.MemUsed, MemTotal: l.MemTotal, SwapUsed: l.SwapUsed, SwapTotal: l.SwapTotal, Load: l.Load, Disk: l.Disk}
	for _, r := range l.Top {
		out.Top = append(out.Top, tui.TopRow{Name: r.Name, CPU: r.CPU, RSS: r.RSS})
	}
	return out
}
