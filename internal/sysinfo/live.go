package sysinfo

import (
	"net"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/TimurTurdyev/tim/internal/paths"
	"github.com/TimurTurdyev/tim/internal/runner"
)

// Сбор текущих показателей машины для macOS и Linux. Разбор вывода - в parse-функциях рядом.

// Memory: занято и всего, байты.
func Memory() (used, total uint64) {
	if runtime.GOOS == "linux" {
		data, _ := os.ReadFile("/proc/meminfo")
		return ParseMeminfo(string(data))
	}
	vm, _ := runner.Output("vm_stat")
	t, _ := runner.Output("sysctl", "-n", "hw.memsize")
	total, _ = strconv.ParseUint(strings.TrimSpace(t), 10, 64)
	return ParseVMStat(vm), total
}

// Load: средняя нагрузка за 1, 5 и 15 минут.
func Load() []float64 {
	if runtime.GOOS == "linux" {
		data, _ := os.ReadFile("/proc/loadavg")
		return ParseLoadavg(string(data))
	}
	out, _ := runner.Output("sysctl", "-n", "vm.loadavg")
	return ParseLoadavg(out)
}

// DataDisk: диск с данными пользователя. На macOS корень - системный том только для чтения.
func DataDisk() (Disk, bool) {
	path := "/"
	if runtime.GOOS == "darwin" {
		path = "/System/Volumes/Data"
	}
	out, err := runner.Output("df", "-k", path)
	if err != nil {
		return Disk{}, false
	}
	return ParseDF(out)
}

// Boot: когда машина загрузилась.
func Boot() (time.Time, bool) {
	if runtime.GOOS == "linux" {
		data, _ := os.ReadFile("/proc/uptime")
		f := strings.Fields(string(data))
		if len(f) == 0 {
			return time.Time{}, false
		}
		sec, err := strconv.ParseFloat(f[0], 64)
		return time.Now().Add(-time.Duration(sec * float64(time.Second))), err == nil
	}
	out, _ := runner.Output("sysctl", "-n", "kern.boottime")
	return ParseBoottime(out)
}

// OS: имя и версия системы, например "macOS 26.1" или "Ubuntu 24.04.1 LTS".
func OS() string {
	if runtime.GOOS == "linux" {
		data, _ := os.ReadFile("/etc/os-release")
		if name := ParseOSRelease(string(data)); name != "" {
			return name
		}
		return "Linux"
	}
	v, _ := runner.Output("sw_vers", "-productVersion")
	return strings.TrimSpace("macOS " + strings.TrimSpace(v))
}

// ParseMeminfo: /proc/meminfo -> занято (MemTotal - MemAvailable) и всего, байты.
func ParseMeminfo(out string) (used, total uint64) {
	var avail uint64
	for _, line := range strings.Split(out, "\n") {
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		f := strings.Fields(v)
		if len(f) == 0 {
			continue
		}
		n, _ := strconv.ParseUint(f[0], 10, 64)
		switch k {
		case "MemTotal":
			total = n * 1024
		case "MemAvailable":
			avail = n * 1024
		}
	}
	if total == 0 || avail > total {
		return 0, total
	}
	return total - avail, total
}

// ParseOSRelease: PRETTY_NAME из /etc/os-release.
func ParseOSRelease(out string) string {
	for _, line := range strings.Split(out, "\n") {
		if v, ok := strings.CutPrefix(line, "PRETTY_NAME="); ok {
			return strings.Trim(v, `"'`)
		}
	}
	return ""
}

// IPv4: первый IPv4-адрес интерфейса.
func IPv4(iface string) string {
	ifc, err := net.InterfaceByName(iface)
	if err != nil {
		return ""
	}
	addrs, _ := ifc.Addrs()
	for _, a := range addrs {
		if n, ok := a.(*net.IPNet); ok && n.IP.To4() != nil {
			return n.IP.String()
		}
	}
	return ""
}

// DockerUp: демон docker отвечает на сокете. Сам docker ps на выключенном движке висит
// или ругается, поэтому сначала ищется сокет.
func DockerUp() bool {
	if os.Getenv("DOCKER_HOST") != "" {
		return true
	}
	home := paths.Home()
	for _, s := range []string{"/var/run/docker.sock", home + "/.orbstack/run/docker.sock", home + "/.docker/run/docker.sock"} {
		if st, err := os.Stat(s); err == nil && st.Mode()&os.ModeSocket != 0 {
			return true
		}
	}
	return false
}

// FailedUnits: упавшие службы systemd; на macOS и без systemd - пусто.
func FailedUnits() []string {
	if runtime.GOOS != "linux" || !runner.Has("systemctl") {
		return nil
	}
	out, _ := runner.Output("systemctl", "list-units", "--state=failed", "--no-legend", "--plain")
	return ParseFailedUnits(out)
}

// ParseFailedUnits: первое поле каждой строки systemctl list-units --no-legend --plain.
func ParseFailedUnits(out string) []string {
	var list []string
	for _, line := range strings.Split(out, "\n") {
		if f := strings.Fields(line); len(f) > 0 {
			list = append(list, strings.TrimPrefix(f[0], "●"))
		}
	}
	return list
}

// Unit: служба systemd.
type Unit struct{ Name, Active, Sub, Description string }

// ParseUnits: systemctl list-units --no-legend --plain.
func ParseUnits(out string) []Unit {
	var list []Unit
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(strings.TrimPrefix(strings.TrimSpace(line), "●"))
		if len(f) < 4 {
			continue
		}
		list = append(list, Unit{Name: strings.TrimSuffix(f[0], ".service"), Active: f[2], Sub: f[3], Description: strings.Join(f[4:], " ")})
	}
	return list
}
