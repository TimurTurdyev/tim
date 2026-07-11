package netinfo

import (
	"net"
	"runtime"
	"strings"

	"github.com/TimurTurdyev/tim/internal/runner"
)

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

// Physical: активный физический интерфейс и его шлюз, пусто если сети нет.
func Physical() (Service, string) {
	svc, ok := PhysIface()
	if !ok {
		return svc, ""
	}
	return svc, IfaceGateway(svc.Device)
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

// IfaceIPv4: первый IPv4-адрес интерфейса, пусто - адреса нет.
func IfaceIPv4(dev string) string {
	ifi, err := net.InterfaceByName(dev)
	if err != nil {
		return ""
	}
	addrs, _ := ifi.Addrs()
	for _, a := range addrs {
		if ipn, ok := a.(*net.IPNet); ok && ipn.IP.To4() != nil {
			return ipn.IP.To4().String()
		}
	}
	return ""
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

// InternetIface: куда уходит трафик в интернет. VPN часто ставит 0/1 и 128.0/1 вместо default,
// поэтому смотрим маршрут до публичного адреса, а не default.
func InternetIface() string { return RouteOf("1.1.1.1").Iface }

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

// ParseIPRouteGet разбирает ip -4 route get <адрес>.
func ParseIPRouteGet(out string) Route {
	f := strings.Fields(strings.SplitN(out, "\n", 2)[0])
	return Route{Gateway: fieldAfter(f, "via"), Iface: fieldAfter(f, "dev")}
}

func RouteOf(addr string) Route {
	if runtime.GOOS == "linux" {
		out, _ := runner.Output("ip", "-4", "route", "get", addr)
		return ParseIPRouteGet(out)
	}
	out, _ := runner.Output("route", "-n", "get", addr)
	return ParseRouteGet(out)
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

func linuxDefault() (dev, gw string) {
	out, _ := runner.Output("ip", "-4", "route", "show", "default")
	return ParseIPRouteDefault(out)
}
