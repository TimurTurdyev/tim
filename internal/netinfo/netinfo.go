// Package netinfo: интерфейсы, шлюзы и маршруты macOS через системные утилиты.
package netinfo

import (
	"net"
	"regexp"
	"runtime"
	"strings"

	"github.com/TimurTurdyev/tim/internal/runner"
)

var ipv4 = regexp.MustCompile(`^\d{1,3}(\.\d{1,3}){3}$`)

var ssProcRe = regexp.MustCompile(`\(\("([^"]+)",pid=(\d+)`)

func IsIPv4(s string) bool { return ipv4.MatchString(s) }

// IsPrivate: 10/8, 172.16/12, 192.168/16. Такие адреса достижимы только через
// VPN, и маршрут в обход им только навредит.
func IsPrivate(ip string) bool {
	parsed := net.ParseIP(ip).To4()
	if parsed == nil {
		return false
	}
	switch {
	case parsed[0] == 10:
		return true
	case parsed[0] == 172 && parsed[1] >= 16 && parsed[1] <= 31:
		return true
	case parsed[0] == 192 && parsed[1] == 168:
		return true
	}
	return false
}

// IsTunnelIface: utun, tun, tap, ppp считаются туннелями.
func IsTunnelIface(name string) bool {
	for _, p := range []string{"utun", "tun", "tap", "ppp", "wg", "tailscale", "zt"} {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

// Service: сетевая служба macOS и её устройство.
type Service struct{ Name, Device string }

// ParseServiceOrder разбирает вывод networksetup -listnetworkserviceorder.
func ParseServiceOrder(out string) []Service {
	var list []Service
	var name string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "(") {
			continue
		}
		if i := strings.Index(line, ") "); i > 0 && !strings.HasPrefix(line, "(Hardware") {
			name = line[i+2:]
		}
		if strings.HasPrefix(line, "(Hardware Port:") {
			if i := strings.Index(line, "Device: "); i >= 0 {
				dev := strings.TrimSuffix(line[i+len("Device: "):], ")")
				list = append(list, Service{Name: name, Device: dev})
			}
		}
	}
	return list
}

// PhysIface: первый активный физический интерфейс из порядка служб.
func PhysIface() (Service, bool) {
	if runtime.GOOS == "linux" {
		dev, _ := linuxDefault()
		return Service{Name: dev, Device: dev}, dev != ""
	}
	out, _ := runner.Output("networksetup", "-listnetworkserviceorder")
	for _, s := range ParseServiceOrder(out) {
		if IsTunnelIface(s.Device) || strings.HasPrefix(s.Device, "bridge") {
			continue
		}
		info, _ := runner.Output("ifconfig", s.Device)
		if strings.Contains(info, "status: active") {
			return s, true
		}
	}
	return Service{}, false
}

// IfaceGateway: роутер интерфейса по DHCP, иначе из таблицы маршрутов.
func IfaceGateway(device string) string {
	if runtime.GOOS == "linux" {
		if dev, gw := linuxDefault(); dev == device {
			return gw
		}
		return ""
	}
	out, _ := runner.Output("ipconfig", "getoption", device, "router")
	gw := strings.TrimSpace(out)
	if gw != "" && gw != "0.0.0.0" {
		return gw
	}
	table, _ := runner.Output("netstat", "-rn", "-f", "inet")
	for _, line := range strings.Split(table, "\n") {
		f := strings.Fields(line)
		if len(f) >= 4 && f[0] == "default" && f[len(f)-1] == device {
			return f[1]
		}
	}
	return ""
}

// Route: куда сейчас уходит адрес.
type Route struct{ Gateway, Iface string }

func ParseRouteGet(out string) Route {
	var r Route
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		switch f[0] {
		case "gateway:":
			r.Gateway = f[1]
		case "interface:":
			r.Iface = f[1]
		}
	}
	return r
}

func RouteOf(addr string) Route {
	if runtime.GOOS == "linux" {
		out, _ := runner.Output("ip", "-4", "route", "get", addr)
		return ParseIPRouteGet(out)
	}
	out, _ := runner.Output("route", "-n", "get", addr)
	return ParseRouteGet(out)
}

// Table: вся таблица IPv4.
func Table() []TableRoute {
	if runtime.GOOS == "linux" {
		out, _ := runner.Output("ip", "-4", "route", "show")
		return ParseIPRoute(out)
	}
	out, _ := runner.Output("netstat", "-rn", "-f", "inet")
	return ParseRoutes(out)
}

// InternetIface: куда уходит трафик в интернет. VPN часто ставит 0/1 и 128.0/1 вместо default,
// поэтому смотрим маршрут до публичного адреса, а не default.
func InternetIface() string { return RouteOf("1.1.1.1").Iface }

// ResolveIPv4 через системный резолвер.
func ResolveIPv4(host string) string {
	if IsIPv4(host) {
		return host
	}
	if ips := LookupIPv4(host); len(ips) > 0 {
		return ips[0]
	}
	return ""
}

// Listening: слушающие TCP-порты через lsof.
type Listener struct {
	Port    string
	PID     string
	Command string
}

func ParseLsof(out string) []Listener {
	seen := map[string]bool{}
	var list []Listener
	for i, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if i == 0 || len(f) < 9 {
			continue
		}
		name := f[len(f)-2]
		if !strings.Contains(name, ":") {
			name = f[len(f)-1]
		}
		port := name[strings.LastIndex(name, ":")+1:]
		if seen[port] {
			continue
		}
		seen[port] = true
		list = append(list, Listener{Port: port, PID: f[1], Command: f[0]})
	}
	return list
}

// Listening: на Linux через ss (lsof там часто не стоит), на macOS через lsof.
// Без root процессы видны только свои: у чужих порт есть, а PID и команды нет.
func Listening() []Listener {
	if runtime.GOOS == "linux" && runner.Has("ss") {
		out, _ := runner.Output("ss", "-Hltnp")
		return ParseSS(out)
	}
	out, _ := runner.Output("lsof", "+c", "0", "-nP", "-iTCP", "-sTCP:LISTEN")
	return ParseLsof(out)
}

// ParseSS: ss -Hltnp -> слушающие порты; один порт на IPv4 и IPv6 - одной строкой.
func ParseSS(out string) []Listener {
	seen := map[string]bool{}
	var list []Listener
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) < 4 {
			continue
		}
		local := f[3]
		port := local[strings.LastIndex(local, ":")+1:]
		if port == "" || seen[port] {
			continue
		}
		seen[port] = true
		l := Listener{Port: port}
		if m := ssProcRe.FindStringSubmatch(line); m != nil {
			l.Command, l.PID = m[1], m[2]
		}
		list = append(list, l)
	}
	return list
}

// PortBusy: кто слушает порт. Если список его не знает (чужой процесс без прав, нет lsof или ss),
// порт пробуется занять: занятый не даст, и тогда он занят неизвестно кем.
func PortBusy(port string) (Listener, bool) {
	for _, l := range Listening() {
		if l.Port == port {
			return l, true
		}
	}
	ln, err := net.Listen("tcp", ":"+port)
	if err != nil {
		return Listener{Port: port, Command: "?"}, true
	}
	_ = ln.Close()
	return Listener{}, false
}
