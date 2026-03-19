package cmd

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"time"

	"github.com/TimurTurdyev/tim/internal/hwinfo"
	"github.com/TimurTurdyev/tim/internal/procs"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/sysinfo"
	"github.com/TimurTurdyev/tim/internal/tui"
)

// Источники Linux: /proc, /sys и nvidia-smi. Без build-тега: на macOS эти функции просто не вызываются.

func procStat() hwinfo.CPUTimes {
	data, _ := os.ReadFile("/proc/stat")
	t, _ := hwinfo.ParseProcStat(string(data))
	return t
}

// cpuBusy: загрузка всех ядер за интервал.
func cpuBusy(d time.Duration) int {
	a := procStat()
	time.Sleep(d)
	return hwinfo.Busy(a, procStat())
}

func linuxSwap() hwinfo.Swap {
	data, _ := os.ReadFile("/proc/meminfo")
	sw, _ := hwinfo.ParseMeminfoSwap(string(data))
	return sw
}

func nvidia() []hwinfo.NvidiaGPU {
	if !runner.Has("nvidia-smi") {
		return nil
	}
	out, _ := runner.Output("nvidia-smi", "--query-gpu=name,utilization.gpu,memory.used,memory.total", "--format=csv,noheader,nounits")
	return hwinfo.ParseNvidiaSmi(out)
}

func hotZone() int {
	files, _ := filepath.Glob("/sys/class/thermal/thermal_zone*/temp")
	var temps []string
	for _, f := range files {
		if data, err := os.ReadFile(f); err == nil {
			temps = append(temps, string(data))
		}
	}
	return hwinfo.HotZone(temps)
}

func collectHWLinux() hwSnapshot {
	s := hwSnapshot{cores: runtime.NumCPU()}
	var wg sync.WaitGroup
	var mu sync.Mutex
	set := func(f func()) { mu.Lock(); f(); mu.Unlock() }
	wg.Go(func() {
		out, _ := runner.Output("lscpu")
		set(func() { s.model = hwinfo.ParseLscpuModel(out) })
	})
	wg.Go(func() {
		used, total := sysinfo.Memory()
		sw := linuxSwap()
		load := sysinfo.Load()
		set(func() { s.memUsed, s.memTotal, s.swap, s.loadAvg = int64(used), int64(total), sw, load })
	})
	wg.Go(func() {
		n := cpuBusy(300 * time.Millisecond)
		set(func() { s.cpuPct = n })
	})
	wg.Go(func() {
		gs := nvidia()
		set(func() {
			for _, g := range gs {
				s.gpus = append(s.gpus, hwinfo.GPU{Model: g.Name})
			}
			if len(gs) > 0 {
				s.gpu = hwinfo.GPULoad{Device: gs[0].Util, InUse: gs[0].MemUsed, Valid: true}
			}
		})
	})
	wg.Go(func() {
		d, _ := sysinfo.DataDisk()
		hot := hotZone()
		set(func() {
			s.diskPct = d.Percent()
			if hot >= 85 {
				s.therm = "горячо: " + strconv.Itoa(hot) + "\u00b0C"
			}
		})
	})
	wg.Wait()
	if s.model == "" {
		s.model = "Linux"
	}
	return s
}

// hwSampleLinux: такт живого экрана - секунда замера /proc/stat.
func hwSampleLinux(base hwSnapshot) tui.HWSample {
	out := tui.HWSample{MemTotal: base.memTotal, Title: hwTitle(base)}
	var wg sync.WaitGroup
	var list []procs.Full
	wg.Go(func() { out.CPU = cpuBusy(time.Second) })
	wg.Go(func() {
		if gs := nvidia(); len(gs) > 0 {
			out.GPU, out.GPUValid, out.VRAM = gs[0].Util, true, gs[0].MemUsed
		}
	})
	wg.Go(func() { list, _ = allProcs() })
	used, _ := sysinfo.Memory()
	sw := linuxSwap()
	out.MemUsed, out.SwapUsed, out.SwapTotal, out.Load = int64(used), sw.Used, sw.Total, sysinfo.Load()
	wg.Wait()
	if base.memTotal > 0 {
		out.Mem = int(out.MemUsed * 100 / base.memTotal)
	}
	out.Top = topRows(list)
	return out
}
