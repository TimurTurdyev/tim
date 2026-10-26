package machine

import (
	"runtime"
	"sync"

	"github.com/TimurTurdyev/tim/internal/app"
	"github.com/TimurTurdyev/tim/internal/procs"
	"github.com/TimurTurdyev/tim/internal/sysinfo"
	"github.com/TimurTurdyev/tim/internal/tui"
)

// HWScreen: Машина на вкладке Железо. Паспорт собирается при первом замере, уже в фоне - открытие
// экрана не ждёт system_profiler.
func HWScreen(env *app.Env) tui.Screen {
	s := machineScreen(0, tui.ProcsOpts{})
	if env.MachinePalette != nil {
		tui.SetPalette(s, env.MachinePalette)
	}
	return s
}

// ProcsScreen: Машина на вкладке Процессы; query - сразу отфильтровать.
func ProcsScreen(query string) tui.Screen { return machineScreen(1, tui.ProcsOpts{Query: query}) }

// MemoryScreen: процессы по памяти; из беды памяти - курсор на держателе и беда над списком.
func MemoryScreen(warning, holder string) tui.Screen {
	return machineScreen(1, tui.ProcsOpts{ByMem: true, Focus: holder, Warning: warning})
}

// machineScreen: вкладки Железо и Процессы на одном экране.
func machineScreen(start int, po tui.ProcsOpts) tui.Screen {
	po.Load, po.Close = loadProcs, closeGroup
	procsTab := tui.NewProcs(po).(tui.Tab)
	src, err := SourceFor()
	if err != nil {
		return tui.NewTabs([]string{"Машина"}, []tui.Tab{procsTab}, 0)
	}
	base := sync.OnceValue(src.Collect)
	hw := tui.NewHW(tui.HWOpts{
		Sample: func() tui.HWSample {
			s := toHWSample(src.Sample(base()))
			if d, ok := sysinfo.DataDisk(); ok {
				s.Disk, s.DiskFree = d.Percent(), int64(d.Avail)
			}
			return s
		},
	})
	return tui.NewTabs([]string{"Машина"}, []tui.Tab{hw, procsTab}, start)
}

func loadProcs() tui.ProcsData {
	var data tui.ProcsData
	list, err := procs.All()
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
	for _, p := range busiestFirst(a.Procs) {
		if procs.Protected(p) {
			g.Protected = true
		}
		g.Procs = append(g.Procs, tui.ProcRow{PID: p.PID, Name: RowName(p), Note: StateNote(p.State), CPU: p.CPU, RSS: p.RSS})
	}
	return g
}

// closeGroup: процессы перечитываются, чтобы не слать сигналы по устаревшим pid.
func closeGroup(g tui.ProcGroup, force bool) (string, error) {
	list, err := procs.All()
	if err != nil {
		return "", err
	}
	for _, a := range procs.Groups(list) {
		if a.Name == g.Name {
			return Close(a, force)
		}
	}
	return g.Name + " уже закрыт", nil
}

// toHWSample: замер в тип экрана - поля те же, пакеты разные.
func toHWSample(l Live) tui.HWSample {
	out := tui.HWSample{Title: l.Title, CPU: l.CPU, GPU: l.GPU, Mem: l.Mem, GPUValid: l.GPUValid, VRAM: l.VRAM,
		MemUsed: l.MemUsed, MemTotal: l.MemTotal, SwapUsed: l.SwapUsed, SwapTotal: l.SwapTotal, Load: l.Load, Disk: l.Disk,
		Cores: l.Cores}
	for _, r := range l.Top {
		out.Top = append(out.Top, tui.TopRow{Name: r.Name, CPU: r.CPU, RSS: r.RSS})
	}
	for _, r := range l.TopMem {
		out.TopMem = append(out.TopMem, tui.TopRow{Name: r.Name, CPU: r.CPU, RSS: r.RSS})
	}
	return out
}
