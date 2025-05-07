package netinfo

import (
	"runtime"
	"strings"

	"github.com/TimurTurdyev/tim/internal/runner"
)

// fieldAfter: значение после слова-ключа в строке ip route ("via 10.0.0.1", "dev eth0").
func fieldAfter(f []string, key string) string {
	for i := 0; i+1 < len(f); i++ {
		if f[i] == key {
			return f[i+1]
		}
	}
	return ""
}

// ParseIPRouteDefault: первый маршрут по умолчанию не через туннель - физический интерфейс и шлюз.
func ParseIPRouteDefault(out string) (dev, gw string) {
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) == 0 || f[0] != "default" {
			continue
		}
		d := fieldAfter(f, "dev")
		if d == "" || IsTunnelIface(d) {
			continue
		}
		return d, fieldAfter(f, "via")
	}
	return "", ""
}

// ParseIPRoute переводит ip -4 route show в строки таблицы как у netstat: с флагами G (через шлюз),
// S (статический, не от ядра) и H (на один адрес).
func ParseIPRoute(out string) []TableRoute {
	var list []TableRoute
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) == 0 || f[0] == "default" || f[0] == "unreachable" || f[0] == "blackhole" {
			continue
		}
		dest, ok := normalizeDest(f[0])
		if !ok {
			continue
		}
		r := TableRoute{Dest: dest, Gateway: fieldAfter(f, "via"), Iface: fieldAfter(f, "dev"), Flags: "U"}
		if r.Gateway != "" {
			r.Flags += "G"
		}
		if fieldAfter(f, "proto") != "kernel" {
			r.Flags += "S"
		}
		if !strings.Contains(dest, "/") {
			r.Flags += "H"
		}
		if r.Gateway == "" {
			r.Gateway = "link#" + r.Iface
		}
		list = append(list, r)
	}
	return list
}

// ParseIPRouteGet разбирает ip -4 route get <адрес>.
func ParseIPRouteGet(out string) Route {
	f := strings.Fields(strings.SplitN(out, "\n", 2)[0])
	return Route{Gateway: fieldAfter(f, "via"), Iface: fieldAfter(f, "dev")}
}

// AddRouteCmd: команда, которая ставит маршрут до адреса или сети через шлюз. На Linux replace
// сразу заменяет старый, на macOS старый снимается отдельно (DelRouteCmd).
func AddRouteCmd(dest, gw, iface string) []string {
	if runtime.GOOS == "linux" {
		args := []string{"ip", "route", "replace", dest, "via", gw}
		if iface != "" {
			args = append(args, "dev", iface)
		}
		return args
	}
	return []string{"route", "-n", "add", kind(dest), dest, gw}
}

// DelRouteCmd: команда, которая снимает маршрут.
func DelRouteCmd(dest string) []string {
	if runtime.GOOS == "linux" {
		return []string{"ip", "route", "del", dest}
	}
	return []string{"route", "-n", "delete", kind(dest), dest}
}

func kind(dest string) string {
	if strings.Contains(dest, "/") {
		return "-net"
	}
	return "-host"
}

func linuxDefault() (dev, gw string) {
	out, _ := runner.Output("ip", "-4", "route", "show", "default")
	return ParseIPRouteDefault(out)
}
