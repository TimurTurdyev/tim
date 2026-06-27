package machine

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"time"

	"github.com/TimurTurdyev/tim/internal/hwinfo"
	"github.com/TimurTurdyev/tim/internal/par"
	"github.com/TimurTurdyev/tim/internal/procs"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/sysinfo"
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

func CollectLinux() Snapshot {
	s := Snapshot{Cores: runtime.NumCPU()}
	par.All(
		func() {
			out, _ := runner.Output("lscpu")
			s.Model = hwinfo.ParseLscpuModel(out)
		},
		func() {
			used, total := sysinfo.Memory()
			s.MemUsed, s.MemTotal = int64(used), int64(total)
			s.Swap, s.LoadAvg = linuxSwap(), sysinfo.Load()
		},
		func() { s.CPUPct = cpuBusy(300 * time.Millisecond) },
		func() {
			gs := nvidia()
			for _, g := range gs {
				s.GPUs = append(s.GPUs, hwinfo.GPU{Model: g.Name})
			}
			if len(gs) > 0 {
				s.GPU = hwinfo.GPULoad{Device: gs[0].Util, InUse: gs[0].MemUsed, Valid: true}
			}
		},
		func() {
			d, _ := sysinfo.DataDisk()
			s.DiskPct = d.Percent()
			if hot := hotZone(); hot >= 85 {
				s.Therm = "горячо: " + strconv.Itoa(hot) + "\u00b0C"
			}
		},
	)
	if s.Model == "" {
		s.Model = "Linux"
	}
	return s
}

// SampleLinux: такт живого экрана - секунда замера /proc/stat.
func SampleLinux(base Snapshot) Live {
	out := Live{MemTotal: base.MemTotal, Title: Title(base)}
	var list []procs.Full
	par.All(
		func() { out.CPU = cpuBusy(time.Second) },
		func() {
			if gs := nvidia(); len(gs) > 0 {
				out.GPU, out.GPUValid, out.VRAM = gs[0].Util, true, gs[0].MemUsed
			}
		},
		func() { list, _ = All() },
		func() {
			used, _ := sysinfo.Memory()
			sw := linuxSwap()
			out.MemUsed, out.SwapUsed, out.SwapTotal, out.Load = int64(used), sw.Used, sw.Total, sysinfo.Load()
		},
	)
	if base.MemTotal > 0 {
		out.Mem = int(out.MemUsed * 100 / base.MemTotal)
	}
	out.Top = TopRows(list)
	return out
}
