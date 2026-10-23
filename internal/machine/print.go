package machine

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/sysinfo"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// printSnapshot: паспорт и загрузка машины одним снимком.
func printSnapshot(asJSON bool) error {
	src, err := SourceFor()
	if err != nil {
		return err
	}
	if asJSON {
		return ui.JSON(src.Collect())
	}
	PrintHW(src.Collect(), true)
	return nil
}

// PrintHW: паспорт и загрузка; full - с диском, экранами, батареей и теплом.
func PrintHW(s Snapshot, full bool) {
	cores := strconv.Itoa(s.Cores) + " ядер"
	if s.PC > 0 && s.EC > 0 {
		cores += ": " + strconv.Itoa(s.PC) + " производительных, " + strconv.Itoa(s.EC) + " эффективных"
	}
	ui.Println(ui.Title(s.Model) + ui.Dim("  "+cores+"  \u2022  "+ui.Bytes(s.MemTotal)))
	ui.Println()
	cpu := hwLabel("процессор") + ui.Bar(s.CPUPct, 24)
	if len(s.LoadAvg) == 3 {
		cpu += ui.Dim(fmt.Sprintf("  load %.1f %.1f %.1f", s.LoadAvg[0], s.LoadAvg[1], s.LoadAvg[2]))
	}
	ui.Println(cpu)
	printGPUs(s, full)
	printMemory(s)
	if !full {
		return
	}
	printDisk(s)
	for _, g := range s.GPUs {
		for _, d := range g.Displays {
			line := hwLabel("экран") + d.Name + ui.Dim("  "+d.Pixels)
			if d.Looks != "" {
				line += ui.Dim(", работает как " + strings.Replace(d.Looks, "Hz", " Гц", 1))
			}
			ui.Println(line)
		}
	}
	if b, ok := ReadBattery(); ok {
		ui.Println(hwLabel("батарея") + fmt.Sprintf("%d%%", b.Charge) + ui.Dim(", "+ui.Count(b.Cycles, "цикл", "цикла", "циклов")+fmt.Sprintf(", ёмкость %d%%", b.Capacity)))
	}
	switch {
	case s.Therm == "" && runtime.GOOS == "linux":
		// Датчики есть не везде, а у виртуалки их нет вовсе: молчание - не "всё хорошо".
	case s.Therm == "":
		ui.Println(hwLabel("тепло") + ui.Dim("ограничений нет"))
	default:
		ui.Println(hwLabel("тепло") + ui.Warn(s.Therm))
	}
}

func printGPUs(s Snapshot, full bool) {
	for _, g := range s.GPUs {
		line := hwLabel("видеокарта") + g.Model
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
	if !s.GPU.Valid {
		return
	}
	ui.Println(hwLabel("") + ui.Bar(s.GPU.Device, 24) + ui.Dim("  видеопамять в работе "+ui.Bytes(s.GPU.InUse)))
	if full && len(s.Holders) > 0 {
		ui.Println(hwLabel("") + ui.Dim("держат: "+strings.Join(s.Holders[:min(4, len(s.Holders))], ", ")))
	}
}

func printMemory(s Snapshot) {
	memPct := 0
	if s.MemTotal > 0 {
		memPct = int(s.MemUsed * 100 / s.MemTotal)
	}
	mem := hwLabel("память") + ui.Bar(memPct, 24) + ui.Dim("  "+ui.Bytes(s.MemUsed)+" из "+ui.Bytes(s.MemTotal))
	if s.FreePct > 0 {
		mem += ui.Dim(", свободно " + strconv.Itoa(s.FreePct) + "%")
	}
	ui.Println(mem)
	if s.Swap.Total > 0 {
		sw := ui.Bytes(s.Swap.Used) + " из " + ui.Bytes(s.Swap.Total)
		// Своп занят больше чем наполовину - памяти не хватает постоянно, а не изредка.
		if s.Swap.Used*2 > s.Swap.Total {
			sw = ui.Warn(sw + ", памяти не хватает")
		}
		ui.Println(hwLabel("swap") + sw)
	}
}

func printDisk(s Snapshot) {
	disk := "занято " + strconv.Itoa(s.DiskPct) + "%"
	if s.Disk.SSD {
		disk = "SSD " + s.Disk.Protocol + ", " + disk
	}
	switch s.Disk.SMART {
	case "Verified":
		disk += ui.Dim(", SMART в порядке")
	case "":
	default:
		disk += ", " + ui.Bad("SMART: "+s.Disk.SMART)
	}
	ui.Println(hwLabel("диск") + disk)
}

func hwLabel(s string) string { return "  " + ui.Label(ui.Pad(s, 12)) }

// ReadBattery: здоровье батареи - system_profiler на macOS, /sys/class/power_supply на Linux.
func ReadBattery() (sysinfo.BatteryHealth, bool) {
	if runtime.GOOS == "darwin" {
		out, err := runner.Output("system_profiler", "SPPowerDataType", "-json")
		if err != nil {
			return sysinfo.BatteryHealth{}, false
		}
		return sysinfo.ParseSystemProfilerPower([]byte(out))
	}
	dirs, _ := filepath.Glob("/sys/class/power_supply/BAT*")
	if len(dirs) == 0 {
		return sysinfo.BatteryHealth{}, false
	}
	files := map[string]string{}
	for _, f := range []string{"capacity", "cycle_count", "energy_full", "energy_full_design", "charge_full", "charge_full_design", "status"} {
		if data, err := os.ReadFile(filepath.Join(dirs[0], f)); err == nil {
			files[f] = string(data)
		}
	}
	return sysinfo.ParseSysBattery(files)
}

func printBattery() {
	b, ok := ReadBattery()
	if !ok {
		ui.Println(ui.Dim("батареи нет"))
		return
	}
	charge := strconv.Itoa(b.Charge) + "%"
	switch {
	case b.Charging:
		charge += ", заряжается"
	case b.Full:
		charge += ", заряжена"
	}
	ui.Println(ui.Label("заряд     ") + charge)
	if b.Charger != "" {
		ui.Println(ui.Label("зарядка   ") + b.Charger)
	}
	cycles := strconv.Itoa(b.Cycles)
	if runtime.GOOS == "darwin" {
		cycles += ui.Dim(" из ~" + strconv.Itoa(sysinfo.DesignCycles) + " по ресурсу Apple")
	}
	ui.Println(ui.Label("циклы     ") + cycles)
	if b.Capacity > 0 {
		capacity := strconv.Itoa(b.Capacity) + "% от заводской"
		// Ниже 80% Apple сама предлагает замену: дальше батарея садится заметно быстрее.
		if b.Capacity < 80 {
			capacity = ui.Warn(capacity + ", Apple считает это поводом для замены")
		}
		ui.Println(ui.Label("ёмкость   ") + capacity)
	}
	if b.Condition != "" {
		cond := b.Condition
		if cond != "Good" {
			cond = ui.Warn(cond)
		}
		ui.Println(ui.Label("состояние ") + cond)
	}
}
