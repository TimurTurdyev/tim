package cmd

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/sysinfo"
	"github.com/TimurTurdyev/tim/internal/ui"
)

func readBattery() (sysinfo.BatteryHealth, bool) {
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

var batteryCmd = &cobra.Command{
	Use:   "battery",
	Short: "Износ батареи: циклы, ёмкость от заводской, состояние",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		b, ok := readBattery()
		if !ok {
			ui.Println(ui.Dim("батареи нет"))
			return nil
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
		return nil
	},
}

func init() {
	root.AddCommand(batteryCmd)
}
