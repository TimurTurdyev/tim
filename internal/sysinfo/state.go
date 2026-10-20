package sysinfo

import (
	"runtime"
	"sort"

	"github.com/TimurTurdyev/tim/internal/runner"
)

// Сводка и главный экран показывают одно и то же о машине - считается здесь, разделы только красят.

// LoadPercent: нагрузка за минуту в долях ядер - 2.1 при 14 ядрах это 15%, а не беда.
func LoadPercent() (int, bool) {
	load := Load()
	if len(load) != 3 {
		return 0, false
	}
	return int(load[0]*100/float64(runtime.NumCPU()) + 0.5), true
}

// BatteryNow: заряд и состояние батареи; на машинах без неё - false.
func BatteryNow() (Battery, bool) {
	if runtime.GOOS != "darwin" {
		return Battery{}, false
	}
	out, err := runner.Output("pmset", "-g", "batt")
	if err != nil {
		return Battery{}, false
	}
	return ParsePmset(out)
}

// Note: что добавить к проценту - заряжается или, что важнее, садится без провода.
func (b Battery) Note() string {
	switch {
	case b.Percent <= 20 && !b.AC:
		return "не заряжается"
	case b.State == "charging":
		return "заряжается"
	}
	return ""
}

// Low: батарея садится и провода нет.
func (b Battery) Low() bool { return b.Percent <= 20 && !b.AC }

// Services: службы машины - на macOS brew services из launchctl, на Linux только упавшие юниты systemd.
// ok=false - узнать не удалось.
func Services() (up, bad []string, ok bool) {
	if runtime.GOOS == "linux" {
		return nil, FailedUnits(), true
	}
	out, err := runner.Output("launchctl", "list")
	if err != nil {
		return nil, nil, false
	}
	for _, a := range ParseLaunchctl(out) {
		switch {
		case a.Running():
			up = append(up, a.Name)
		case a.Status != 0:
			bad = append(bad, a.Name)
		}
	}
	sort.Strings(up)
	return up, bad, true
}
