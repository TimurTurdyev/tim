// Package netinfo: интерфейсы, шлюзы и маршруты macOS через системные утилиты.
package netinfo

import (
	"net"
	"regexp"
	"strings"

	"github.com/TimurTurdyev/tim/internal/runner"
)

var ipv4 = regexp.MustCompile(`^\d{1,3}(\.\d{1,3}){3}$`)

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
	for _, p := range []string{"utun", "tun", "tap", "ppp"} {
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
		switch {
		case strings.HasPrefix(line, "("):
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
	}
	return list
}

// PhysIface: первый активный физический интерфейс из порядка служб.
func PhysIface() (Service, bool) {
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
	out, _ := runner.Output("route", "-n", "get", addr)
	return ParseRouteGet(out)
}

// HostRoute: статический host-маршрут из таблицы.
type HostRoute struct{ Dest, Gateway, Iface string }

func ParseHostRoutes(out string) []HostRoute {
	var list []HostRoute
	for _, r := range ParseRoutes(out) {
		if !r.IsNet() && strings.Contains(r.Flags, "H") && strings.Contains(r.Flags, "S") {
			list = append(list, HostRoute{Dest: r.Dest, Gateway: r.Gateway, Iface: r.Iface})
		}
	}
	return list
}

func HostRoutes() []HostRoute {
	out, _ := runner.Output("netstat", "-rn", "-f", "inet")
	return ParseHostRoutes(out)
}

func DefaultIface() string { return RouteOf("default").Iface }

// Table: вся таблица IPv4.
func Table() []TableRoute {
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
	out, _ := runner.Output("dscacheutil", "-q", "host", "-a", "name", host)
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "ip_address:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "ip_address:"))
		}
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

func Listening() []Listener {
	out, _ := runner.Output("lsof", "+c", "0", "-nP", "-iTCP", "-sTCP:LISTEN")
	return ParseLsof(out)
}

func PortBusy(port string) (Listener, bool) {
	for _, l := range Listening() {
		if l.Port == port {
			return l, true
		}
	}
	return Listener{}, false
}
