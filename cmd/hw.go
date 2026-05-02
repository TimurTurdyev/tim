package cmd

import (
	"fmt"
	"runtime"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/TimurTurdyev/tim/internal/machine"
	"github.com/TimurTurdyev/tim/internal/tui"
	"github.com/TimurTurdyev/tim/internal/ui"
)

var hwWatch bool

func printHW(s machine.Snapshot, full bool) {
	cores := strconv.Itoa(s.Cores) + " ядер"
	if s.PC > 0 && s.EC > 0 {
		cores += ": " + strconv.Itoa(s.PC) + " производительных, " + strconv.Itoa(s.EC) + " эффективных"
	}
	ui.Println(ui.Title(s.Model) + ui.Dim("  "+cores+"  \u2022  "+ui.Bytes(s.MemTotal)))
	ui.Println()
	label := func(s string) string { return "  " + ui.Label(ui.Pad(s, 12)) }
	cpu := label("процессор") + ui.Bar(s.CPUPct, 24)
	if len(s.LoadAvg) == 3 {
		cpu += ui.Dim(fmt.Sprintf("  load %.1f %.1f %.1f", s.LoadAvg[0], s.LoadAvg[1], s.LoadAvg[2]))
	}
	ui.Println(cpu)
	for _, g := range s.GPUs {
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
	if s.GPU.Valid {
		ui.Println(label("") + ui.Bar(s.GPU.Device, 24) + ui.Dim("  видеопамять в работе "+ui.Bytes(s.GPU.InUse)))
		if full && len(s.Holders) > 0 {
			ui.Println(label("") + ui.Dim("держат: "+strings.Join(s.Holders[:min(4, len(s.Holders))], ", ")))
		}
	}
	memPct := 0
	if s.MemTotal > 0 {
		memPct = int(s.MemUsed * 100 / s.MemTotal)
	}
	mem := label("память") + ui.Bar(memPct, 24) + ui.Dim("  "+ui.Bytes(s.MemUsed)+" из "+ui.Bytes(s.MemTotal))
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
		ui.Println(label("swap") + sw)
	}
	if !full {
		return
	}
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
	ui.Println(label("диск") + disk)
	for _, g := range s.GPUs {
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
	switch {
	case s.Therm == "" && runtime.GOOS == "linux":
		// Датчики есть не везде, а у виртуалки их нет вовсе: молчание - не "всё хорошо".
	case s.Therm == "":
		ui.Println(label("тепло") + ui.Dim("ограничений нет"))
	default:
		ui.Println(label("тепло") + ui.Warn(s.Therm))
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
		collect, _, err := machine.Funcs()
		if err != nil {
			return err
		}
		if !hwWatch || !tui.Available() {
			// Без терминала живому экрану негде жить - один снимок.
			printHW(collect(), true)
			return nil
		}
		return runScreen(hwScreen())
	},
}

func init() {
	hwCmd.Flags().BoolVarP(&hwWatch, "watch", "w", false, "обновлять раз в секунду")
	root.AddCommand(hwCmd)
}
