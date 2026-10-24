// Package machine: железо и процессы этой машины - снимок, живой замер, закрытие программ.
package machine

import (
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/TimurTurdyev/tim/internal/hwinfo"
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

// Live: один замер для живого экрана; экран в cmd переводит его в свой тип.
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
	sort.Slice(hs, func(i, j int) bool { return hs[i].rss > hs[j].rss })
	var names []string
	for _, x := range hs {
		names = append(names, x.name+" "+ui.Bytes(x.rss))
	}
	return names
}

// Collect: всё разом и параллельно; загрузка процессора - по ps, без секундного iostat.
func Collect() Snapshot {
	var s Snapshot
	var wg sync.WaitGroup
	var mu sync.Mutex
	set := func(f func()) { mu.Lock(); f(); mu.Unlock() }
	wg.Go(func() {
		m, _ := runner.Output("sysctl", "-n", "machdep.cpu.brand_string")
		c, pc, ec := sysctlInt("hw.ncpu"), sysctlInt("hw.perflevel0.physicalcpu"), sysctlInt("hw.perflevel1.physicalcpu")
		set(func() { s.Model, s.Cores, s.PC, s.EC = strings.TrimSpace(m), c, pc, ec })
	})
	wg.Go(func() {
		used, total := sysinfo.Memory()
		set(func() { s.MemUsed, s.MemTotal = int64(used), int64(total) })
	})
	wg.Go(func() {
		out, _ := runner.Output("memory_pressure")
		n, _ := hwinfo.ParseFreePercent(out)
		sw, _ := runner.Output("sysctl", "-n", "vm.swapusage")
		swap, _ := hwinfo.ParseSwap(sw)
		set(func() { s.FreePct, s.Swap = n, swap })
	})
	wg.Go(func() {
		l := sysinfo.Load()
		set(func() { s.LoadAvg = l })
	})
	wg.Go(func() {
		list, err := All()
		if err != nil {
			return
		}
		var sum float64
		for _, p := range list {
			sum += p.CPU
		}
		ncpu := runtime.NumCPU()
		holders := gpuHolders(list)
		set(func() { s.CPUPct, s.Holders = int(sum/float64(ncpu)+0.5), holders })
	})
	wg.Go(func() {
		out, _ := runner.Output("system_profiler", "SPDisplaysDataType", "-json")
		set(func() { s.GPUs = hwinfo.ParseDisplays([]byte(out)) })
	})
	wg.Go(func() {
		out, _ := runner.Output("ioreg", "-r", "-d", "1", "-w", "0", "-c", "IOAccelerator")
		set(func() { s.GPU = hwinfo.ParseGPULoad(out) })
	})
	wg.Go(func() {
		out, _ := runner.Output("diskutil", "info", "/")
		d, _ := sysinfo.DataDisk()
		th, _ := runner.Output("pmset", "-g", "therm")
		set(func() { s.Disk, s.DiskPct, s.Therm = hwinfo.ParseDiskutil(out), d.Percent(), hwinfo.ParseTherm(th) })
	})
	wg.Wait()
	return s
}

// Funcs: сбор паспорта и замер для этой системы.
func Funcs() (func() Snapshot, func(Snapshot) Live, error) {
	switch runtime.GOOS {
	case "darwin":
		return Collect, Sample, nil
	case "linux":
		return CollectLinux, SampleLinux, nil
	}
	return nil, nil, ui.Errorf("на %s пока не умею", runtime.GOOS)
}

// Sample: замер для живого экрана. Паспорт машины уже собран, здесь только то, что меняется,
// и всё параллельно с секундным iostat - он и задаёт такт.
func Sample(base Snapshot) Live {
	out := Live{MemTotal: base.MemTotal, Title: Title(base)}
	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		used int64
		list []procs.Full
	)
	set := func(f func()) { mu.Lock(); f(); mu.Unlock() }
	wg.Go(func() {
		o, _ := runner.Output("iostat", "-c", "2", "-w", "1")
		n, _ := hwinfo.ParseIostatCPU(o)
		set(func() { out.CPU = n })
	})
	wg.Go(func() {
		o, _ := runner.Output("ioreg", "-r", "-d", "1", "-w", "0", "-c", "IOAccelerator")
		g := hwinfo.ParseGPULoad(o)
		set(func() { out.GPU, out.GPUValid, out.VRAM = g.Device, g.Valid, g.InUse })
	})
	wg.Go(func() {
		u, _ := sysinfo.Memory()
		sw, _ := runner.Output("sysctl", "-n", "vm.swapusage")
		swap, _ := hwinfo.ParseSwap(sw)
		la := sysinfo.Load()
		set(func() {
			used = int64(u)
			out.SwapUsed, out.SwapTotal, out.Load = swap.Used, swap.Total, la
		})
	})
	wg.Go(func() {
		l, _ := All()
		set(func() { list = l })
	})
	wg.Wait()
	out.MemUsed = used
	if base.MemTotal > 0 {
		out.Mem = int(used * 100 / base.MemTotal)
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
