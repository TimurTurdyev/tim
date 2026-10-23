// Package machine: железо и процессы этой машины - снимок, живой замер, закрытие программ.
package machine

import (
	"cmp"
	"runtime"
	"slices"
	"strconv"
	"strings"

	"github.com/TimurTurdyev/tim/internal/hwinfo"
	"github.com/TimurTurdyev/tim/internal/par"
	"github.com/TimurTurdyev/tim/internal/procs"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/sysinfo"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// Snapshot: паспорт машины и загрузка в момент снимка.
type Snapshot struct {
	Model         string
	Cores, PC, EC int
	MemTotal      int64
	MemUsed       int64
	FreePct       int
	Swap          hwinfo.Swap
	LoadAvg       []float64
	CPUPct        int
	GPUs          []hwinfo.GPU
	GPU           hwinfo.GPULoad
	Holders       []string
	Disk          hwinfo.Disk
	DiskPct       int
	Therm         string
}

// Live: один замер для живого экрана; экран переводит его в свой тип.
type Live struct {
	Title               string
	CPU, GPU, Mem       int
	GPUValid            bool
	VRAM                int64
	MemUsed, MemTotal   int64
	SwapUsed, SwapTotal int64
	Load                []float64
	Disk                int
	Top                 []TopRow
}

// TopRow: строка самых занятых.
type TopRow struct {
	Name string
	CPU  float64
	RSS  int64
}

func sysctlInt(name string) int {
	out, _ := runner.Output("sysctl", "-n", name)
	n, _ := strconv.Atoi(strings.TrimSpace(out))
	return n
}

// gpuHolders: свои процессы, которые держат видеокарту, тяжёлые первыми. Системные службы не показываем:
// они держат её всегда, и сделать с ними ничего нельзя.
func gpuHolders(list []procs.Full) []string {
	out, _ := runner.Output("ioreg", "-r", "-c", "AGXDeviceUserClient", "-w", "0")
	byPID := map[string]procs.Full{}
	for _, p := range list {
		byPID[p.PID] = p
	}
	type h struct {
		name string
		rss  int64
	}
	// Процессы одного приложения - одной строкой, память суммой: у браузера их несколько.
	idx := map[string]int{}
	var hs []h
	for _, c := range hwinfo.ParseGPUClients(out) {
		p, ok := byPID[c.PID]
		if !ok || p.UID == 0 || procs.Protected(p) || strings.HasPrefix(p.Comm, "/System/") || strings.HasPrefix(p.Comm, "/usr/libexec/") {
			continue
		}
		name := procs.Name(p.Comm, p.Args)
		if i, seen := idx[name]; seen {
			hs[i].rss += p.RSS
			continue
		}
		idx[name] = len(hs)
		hs = append(hs, h{name, p.RSS})
	}
	slices.SortStableFunc(hs, func(a, b h) int { return cmp.Or(cmp.Compare(b.rss, a.rss), cmp.Compare(a.name, b.name)) })
	var names []string
	for _, x := range hs {
		names = append(names, x.name+" "+ui.Bytes(x.rss))
	}
	return names
}

// Collect: всё разом и параллельно; загрузка процессора - по ps, без секундного iostat.
func (darwin) Collect() Snapshot {
	var s Snapshot
	// Каждый замер пишет свои поля, поэтому общий замок не нужен.
	par.All(
		func() {
			m, _ := runner.Output("sysctl", "-n", "machdep.cpu.brand_string")
			s.Model = strings.TrimSpace(m)
			s.Cores, s.PC, s.EC = sysctlInt("hw.ncpu"), sysctlInt("hw.perflevel0.physicalcpu"), sysctlInt("hw.perflevel1.physicalcpu")
		},
		func() {
			used, total := sysinfo.Memory()
			s.MemUsed, s.MemTotal = int64(used), int64(total)
		},
		func() {
			out, _ := runner.Output("memory_pressure")
			s.FreePct, _ = hwinfo.ParseFreePercent(out)
			sw, _ := runner.Output("sysctl", "-n", "vm.swapusage")
			s.Swap, _ = hwinfo.ParseSwap(sw)
		},
		func() { s.LoadAvg = sysinfo.Load() },
		func() {
			list, err := procs.All()
			if err != nil {
				return
			}
			var sum float64
			for _, p := range list {
				sum += p.CPU
			}
			s.CPUPct = int(sum/float64(runtime.NumCPU()) + 0.5)
			s.Holders = gpuHolders(list)
		},
		func() {
			out, _ := runner.Output("system_profiler", "SPDisplaysDataType", "-json")
			s.GPUs = hwinfo.ParseDisplays([]byte(out))
		},
		func() {
			out, _ := runner.Output("ioreg", "-r", "-d", "1", "-w", "0", "-c", "IOAccelerator")
			s.GPU = hwinfo.ParseGPULoad(out)
		},
		func() {
			out, _ := runner.Output("diskutil", "info", "/")
			s.Disk = hwinfo.ParseDiskutil(out)
			d, _ := sysinfo.DataDisk()
			s.DiskPct = d.Percent()
			th, _ := runner.Output("pmset", "-g", "therm")
			s.Therm = hwinfo.ParseTherm(th)
		},
	)
	return s
}

// Source: паспорт машины и замеры для живого экрана - у каждой системы свои утилиты.
type Source interface {
	Collect() Snapshot
	Sample(base Snapshot) Live
}

// darwin: macOS через sysctl, ioreg, system_profiler и iostat.
type darwin struct{}

// SourceFor: источник для этой системы.
func SourceFor() (Source, error) {
	switch runtime.GOOS {
	case "darwin":
		return darwin{}, nil
	case "linux":
		return linux{}, nil
	}
	return nil, ui.Errorf("на %s пока не умею", runtime.GOOS)
}

// Sample: замер для живого экрана. Паспорт машины уже собран, здесь только то, что меняется,
// и всё параллельно с секундным iostat - он и задаёт такт.
func (darwin) Sample(base Snapshot) Live {
	out := Live{MemTotal: base.MemTotal, Title: Title(base)}
	var list []procs.Full
	par.All(
		func() {
			o, _ := runner.Output("iostat", "-c", "2", "-w", "1")
			out.CPU, _ = hwinfo.ParseIostatCPU(o)
		},
		func() {
			o, _ := runner.Output("ioreg", "-r", "-d", "1", "-w", "0", "-c", "IOAccelerator")
			g := hwinfo.ParseGPULoad(o)
			out.GPU, out.GPUValid, out.VRAM = g.Device, g.Valid, g.InUse
		},
		func() {
			used, _ := sysinfo.Memory()
			out.MemUsed = int64(used)
			sw, _ := runner.Output("sysctl", "-n", "vm.swapusage")
			swap, _ := hwinfo.ParseSwap(sw)
			out.SwapUsed, out.SwapTotal = swap.Used, swap.Total
			out.Load = sysinfo.Load()
		},
		func() { list, _ = procs.All() },
	)
	if base.MemTotal > 0 {
		out.Mem = int(out.MemUsed * 100 / base.MemTotal)
	}
	out.Top = TopRows(list)
	return out
}

// TopRows: самые занятые приложения для живого экрана.
func TopRows(list []procs.Full) []TopRow {
	samples := make([]procs.Sample, len(list))
	for i, p := range list {
		samples[i] = p.Sample
	}
	groups := procs.GroupBy(samples, func(p procs.Sample) string { return procs.Name(p.Comm, p.Args) }, true)
	var rows []TopRow
	for _, g := range groups[:min(6, len(groups))] {
		rows = append(rows, TopRow{Name: g.Name, CPU: g.CPU, RSS: g.RSS})
	}
	return rows
}

func Title(s Snapshot) string {
	return s.Model + " \u00b7 " + strconv.Itoa(s.Cores) + " ядер \u00b7 " + ui.Bytes(s.MemTotal)
}

// SwapNow: занятый своп; без sysctl (не macOS) - false.
func SwapNow() (hwinfo.Swap, bool) {
	out, err := runner.Output("sysctl", "-n", "vm.swapusage")
	if err != nil {
		return hwinfo.Swap{}, false
	}
	return hwinfo.ParseSwap(out)
}

// MemHolder: приложение, которое держит больше всех памяти; без замера CPU, поэтому быстро.
func MemHolder() string {
	out, err := runner.Output("ps", procs.PsAll("pid=,ppid=,uid=,rss=,pcpu=,etime=,state=,comm=")...)
	if err != nil {
		return ""
	}
	top := procs.App{}
	for _, g := range procs.Groups(procs.ParseFull(out)) {
		if g.RSS > top.RSS {
			top = g
		}
	}
	return top.Name
}
