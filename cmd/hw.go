package cmd

import (
	"errors"
	"fmt"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/spf13/cobra"

	"github.com/TimurTurdyev/tim/internal/hwinfo"
	"github.com/TimurTurdyev/tim/internal/procs"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/sysinfo"
	"github.com/TimurTurdyev/tim/internal/tui"
	"github.com/TimurTurdyev/tim/internal/ui"
)

var hwWatch bool

// bar: полоска загрузки в фирменных цветах: спокойно, внимание, плохо.
func bar(pct, width int) string {
	pct = max(0, min(100, pct))
	full := pct * width / 100
	head := strings.Repeat("\u2588", full)
	var b string
	switch {
	case pct >= 85:
		b = ui.Bad(head) + ui.Dim(strings.Repeat("\u2591", width-full))
	case pct >= 60:
		b = ui.Warn(head) + ui.Dim(strings.Repeat("\u2591", width-full))
	default:
		b = ui.OK(head) + ui.Dim(strings.Repeat("\u2591", width-full))
	}
	return b + " " + ui.Pad(strconv.Itoa(pct)+"%", 5)
}

type hwSnapshot struct {
	model         string
	cores, pc, ec int
	memTotal      int64
	memUsed       int64
	freePct       int
	swap          hwinfo.Swap
	loadAvg       []float64
	cpuPct        int
	gpus          []hwinfo.GPU
	gpu           hwinfo.GPULoad
	holders       []string
	disk          hwinfo.Disk
	diskPct       int
	therm         string
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
		names = append(names, x.name+" "+fmtBytes(x.rss))
	}
	return names
}

// collectHW: всё разом и параллельно; cpuNow - мерить ли загрузку процессора за секунду (iostat).
func collectHW(cpuNow bool) hwSnapshot {
	var s hwSnapshot
	var wg sync.WaitGroup
	var mu sync.Mutex
	set := func(f func()) { mu.Lock(); f(); mu.Unlock() }
	wg.Go(func() {
		m, _ := runner.Output("sysctl", "-n", "machdep.cpu.brand_string")
		c, pc, ec := sysctlInt("hw.ncpu"), sysctlInt("hw.perflevel0.physicalcpu"), sysctlInt("hw.perflevel1.physicalcpu")
		total := sysctlInt("hw.memsize")
		set(func() { s.model, s.cores, s.pc, s.ec, s.memTotal = strings.TrimSpace(m), c, pc, ec, int64(total) })
	})
	wg.Go(func() {
		vm, _ := runner.Output("vm_stat")
		set(func() { s.memUsed = int64(sysinfo.ParseVMStat(vm)) })
	})
	wg.Go(func() {
		out, _ := runner.Output("memory_pressure")
		n, _ := hwinfo.ParseFreePercent(out)
		sw, _ := runner.Output("sysctl", "-n", "vm.swapusage")
		swap, _ := hwinfo.ParseSwap(sw)
		set(func() { s.freePct, s.swap = n, swap })
	})
	wg.Go(func() {
		out, _ := runner.Output("sysctl", "-n", "vm.loadavg")
		set(func() { s.loadAvg = sysinfo.ParseLoadavg(out) })
	})
	wg.Go(func() {
		if cpuNow {
			out, _ := runner.Output("iostat", "-c", "2", "-w", "1")
			n, _ := hwinfo.ParseIostatCPU(out)
			set(func() { s.cpuPct = n })
			return
		}
		list, err := allProcs()
		if err != nil {
			return
		}
		var sum float64
		for _, p := range list {
			sum += p.CPU
		}
		ncpu := runtime.NumCPU()
		holders := gpuHolders(list)
		set(func() { s.cpuPct, s.holders = int(sum/float64(ncpu)+0.5), holders })
	})
	wg.Go(func() {
		out, _ := runner.Output("system_profiler", "SPDisplaysDataType", "-json")
		set(func() { s.gpus = hwinfo.ParseDisplays([]byte(out)) })
	})
	wg.Go(func() {
		out, _ := runner.Output("ioreg", "-r", "-d", "1", "-w", "0", "-c", "IOAccelerator")
		set(func() { s.gpu = hwinfo.ParseGPULoad(out) })
	})
	wg.Go(func() {
		out, _ := runner.Output("diskutil", "info", "/")
		df, _ := runner.Output("df", "-k", "/System/Volumes/Data")
		d, _ := sysinfo.ParseDF(df)
		th, _ := runner.Output("pmset", "-g", "therm")
		set(func() { s.disk, s.diskPct, s.therm = hwinfo.ParseDiskutil(out), d.Percent(), hwinfo.ParseTherm(th) })
	})
	wg.Wait()
	return s
}

func printHW(s hwSnapshot, full bool) {
	cores := strconv.Itoa(s.cores) + " ядер"
	if s.pc > 0 && s.ec > 0 {
		cores += ": " + strconv.Itoa(s.pc) + " производительных, " + strconv.Itoa(s.ec) + " эффективных"
	}
	ui.Println(ui.Title(s.model) + ui.Dim("  "+cores+"  \u2022  "+fmtBytes(s.memTotal)))
	ui.Println()
	label := func(s string) string { return "  " + ui.Label(ui.Pad(s, 12)) }
	cpu := label("процессор") + bar(s.cpuPct, 24)
	if len(s.loadAvg) == 3 {
		cpu += ui.Dim(fmt.Sprintf("  load %.1f %.1f %.1f", s.loadAvg[0], s.loadAvg[1], s.loadAvg[2]))
	}
	ui.Println(cpu)
	for _, g := range s.gpus {
		line := label("видеокарта") + g.Model
		var extra []string
		if g.Cores > 0 {
			extra = append(extra, strconv.Itoa(g.Cores)+" ядер")
		}
		if g.Metal != "" {
			extra = append(extra, g.Metal)
		}
		if len(extra) > 0 {
			line += ui.Dim(", " + strings.Join(extra, ", "))
		}
		ui.Println(line)
	}
	if s.gpu.Valid {
		ui.Println(label("") + bar(s.gpu.Device, 24) + ui.Dim("  видеопамять в работе "+fmtBytes(s.gpu.InUse)))
		if full && len(s.holders) > 0 {
			ui.Println(label("") + ui.Dim("держат: "+strings.Join(s.holders[:min(4, len(s.holders))], ", ")))
		}
	}
	memPct := 0
	if s.memTotal > 0 {
		memPct = int(s.memUsed * 100 / s.memTotal)
	}
	mem := label("память") + bar(memPct, 24) + ui.Dim("  "+fmtBytes(s.memUsed)+" из "+fmtBytes(s.memTotal))
	if s.freePct > 0 {
		mem += ui.Dim(", свободно " + strconv.Itoa(s.freePct) + "%")
	}
	ui.Println(mem)
	if s.swap.Total > 0 {
		sw := fmtBytes(s.swap.Used) + " из " + fmtBytes(s.swap.Total)
		// Своп занят больше чем наполовину - памяти не хватает постоянно, а не изредка.
		if s.swap.Used*2 > s.swap.Total {
			sw = ui.Warn(sw + ", памяти не хватает")
		}
		ui.Println(label("swap") + sw)
	}
	if !full {
		return
	}
	disk := "занято " + strconv.Itoa(s.diskPct) + "%"
	if s.disk.SSD {
		disk = "SSD " + s.disk.Protocol + ", " + disk
	}
	switch s.disk.SMART {
	case "Verified":
		disk += ui.Dim(", SMART в порядке")
	case "":
	default:
		disk += ", " + ui.Bad("SMART: "+s.disk.SMART)
	}
	ui.Println(label("диск") + disk)
	for _, g := range s.gpus {
		for _, d := range g.Displays {
			line := label("экран") + d.Name + ui.Dim("  "+d.Pixels)
			if d.Looks != "" {
				line += ui.Dim(", работает как " + strings.Replace(d.Looks, "Hz", " Гц", 1))
			}
			ui.Println(line)
		}
	}
	if b, ok := readBattery(); ok {
		ui.Println(label("батарея") + fmt.Sprintf("%d%%", b.Charge) + ui.Dim(fmt.Sprintf(", %d циклов, ёмкость %d%%", b.Cycles, b.Capacity)))
	}
	if s.therm == "" {
		ui.Println(label("тепло") + ui.Dim("ограничений нет"))
	} else {
		ui.Println(label("тепло") + ui.Warn(s.therm))
	}
}

var hwCmd = &cobra.Command{
	Use:   "hw [--watch]",
	Short: "Железо и его загрузка: процессор, видеокарта, память, swap, диск, экраны, тепло",
	Long: `Паспорт машины и текущая загрузка. У видеокарты видно, насколько она занята и сколько видеопамяти
в работе, и какие твои процессы её держат (сколько каждый грузит - без root не узнать).
--watch обновляет процессор, видеокарту и память раз в секунду; выход - Ctrl-C.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if runtime.GOOS != "darwin" {
			return ui.Errorf("пока только macOS")
		}
		if !hwWatch {
			printHW(collectHW(false), true)
			return nil
		}
		s0 := collectHW(false)
		title := s0.model + "  " + strconv.Itoa(s0.cores) + " ядер  " + fmtBytes(s0.memTotal)
		err := tui.RunHW(title, func() tui.HWSample { return hwSample(s0) })
		if errors.Is(err, tui.ErrNoTTY) {
			// Без терминала живому экрану негде жить - один снимок.
			printHW(s0, true)
			return nil
		}
		return err
	},
}

func init() {
	hwCmd.Flags().BoolVarP(&hwWatch, "watch", "w", false, "обновлять раз в секунду")
	root.AddCommand(hwCmd)
}

// hwSample: замер для живого экрана. Паспорт машины уже собран, здесь только то, что меняется.
func hwSample(base hwSnapshot) tui.HWSample {
	s := collectHW(true)
	out := tui.HWSample{CPU: s.cpuPct, GPU: s.gpu.Device, GPUValid: s.gpu.Valid, VRAM: s.gpu.InUse,
		MemUsed: s.memUsed, MemTotal: base.memTotal, SwapUsed: s.swap.Used, SwapTotal: s.swap.Total, Load: s.loadAvg}
	if base.memTotal > 0 {
		out.Mem = int(s.memUsed * 100 / base.memTotal)
	}
	if list, err := allProcs(); err == nil {
		samples := make([]procs.Sample, len(list))
		for i, p := range list {
			samples[i] = p.Sample
		}
		groups := procs.GroupBy(samples, func(p procs.Sample) string { return procs.Name(p.Comm, p.Args) }, true)
		for _, g := range groups[:min(6, len(groups))] {
			out.Top = append(out.Top, tui.TopRow{Name: g.Name, CPU: g.CPU, RSS: g.RSS})
		}
	}
	return out
}
