package netinfo

import (
	"net"
	"os"
	"runtime"
	"strings"

	"github.com/TimurTurdyev/tim/internal/runner"
)

// Tunnel: туннельный интерфейс с IPv4 или глобальным IPv6. Служебные utun macOS с одним fe80:: не считаются.
type Tunnel struct {
	Iface string
	IPv4  []string
	IPv6  []string
}

func ParseIfconfigTunnels(out string) []Tunnel {
	var list []Tunnel
	var cur *Tunnel
	flush := func() {
		if cur != nil && (len(cur.IPv4) > 0 || len(cur.IPv6) > 0) {
			list = append(list, *cur)
		}
		cur = nil
	}
	for _, line := range strings.Split(out, "\n") {
		if line != "" && line[0] != '\t' && line[0] != ' ' {
			flush()
			name, _, ok := strings.Cut(line, ":")
			if ok && IsTunnelIface(name) {
				cur = &Tunnel{Iface: name}
			}
			continue
		}
		if cur == nil {
			continue
		}
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		switch f[0] {
		case "inet":
			cur.IPv4 = append(cur.IPv4, f[1])
		case "inet6":
			if !strings.HasPrefix(f[1], "fe80:") {
				cur.IPv6 = append(cur.IPv6, f[1])
			}
		}
	}
	flush()
	return list
}

func Tunnels() []Tunnel {
	if runtime.GOOS == "linux" {
		return linuxTunnels()
	}
	out, _ := runner.Output("ifconfig")
	return ParseIfconfigTunnels(out)
}

// linuxTunnels: ifconfig из net-tools на свежих системах не стоит, адреса берутся у ядра напрямую.
func linuxTunnels() []Tunnel {
	ifs, _ := net.Interfaces()
	var list []Tunnel
	for _, ifc := range ifs {
		if !IsTunnelIface(ifc.Name) || ifc.Flags&net.FlagUp == 0 {
			continue
		}
		t := Tunnel{Iface: ifc.Name}
		addrs, _ := ifc.Addrs()
		for _, a := range addrs {
			n, ok := a.(*net.IPNet)
			switch {
			case !ok:
			case n.IP.To4() != nil:
				t.IPv4 = append(t.IPv4, n.IP.String())
			case n.IP.IsGlobalUnicast():
				t.IPv6 = append(t.IPv6, n.IP.String())
			}
		}
		if len(t.IPv4) > 0 || len(t.IPv6) > 0 {
			list = append(list, t)
		}
	}
	return list
}

// NetConn: VPN-подключение из настроек macOS.
type NetConn struct {
	Name  string
	State string
}

// ParseNcList разбирает scutil --nc list: состояние в скобках, имя в кавычках.
func ParseNcList(out string) []NetConn {
	var list []NetConn
	for _, line := range strings.Split(out, "\n") {
		i := strings.Index(line, "(")
		j := strings.Index(line, ")")
		q1 := strings.Index(line, `"`)
		if i < 0 || j < i || q1 < 0 {
			continue
		}
		q2 := strings.Index(line[q1+1:], `"`)
		if q2 < 0 {
			continue
		}
		list = append(list, NetConn{Name: line[q1+1 : q1+1+q2], State: line[i+1 : j]})
	}
	return list
}

func NetConns() []NetConn {
	if runtime.GOOS == "linux" {
		return nmcliVPN()
	}
	out, _ := runner.Output("scutil", "--nc", "list")
	return ParseNcList(out)
}

// ParseDNS: серверы первого резолвера без домена из scutil --dns, то есть того, что обслуживает всё.
func ParseDNS(out string) []string {
	var servers []string
	inResolver, scoped := false, false
	for _, line := range strings.Split(out, "\n") {
		t := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(t, "resolver #"):
			if inResolver && !scoped && len(servers) > 0 {
				return servers
			}
			inResolver, scoped, servers = true, false, nil
		case strings.HasPrefix(t, "DNS configuration (for scoped queries)"):
			return servers
		case strings.HasPrefix(t, "domain"):
			scoped = true
		case strings.HasPrefix(t, "nameserver["):
			if _, v, ok := strings.Cut(t, ":"); ok {
				servers = append(servers, strings.TrimSpace(v))
			}
		}
	}
	if scoped {
		return nil
	}
	return servers
}

func DNSServers() []string {
	if runtime.GOOS == "linux" {
		return linuxDNS()
	}
	out, _ := runner.Output("scutil", "--dns")
	return ParseDNS(out)
}

// linuxDNS: из resolv.conf; если там заглушка systemd-resolved, то её настоящие серверы.
func linuxDNS() []string {
	data, _ := os.ReadFile("/etc/resolv.conf")
	list := ParseResolvConf(string(data))
	if len(list) == 1 && list[0] == "127.0.0.53" {
		if data, err := os.ReadFile("/run/systemd/resolve/resolv.conf"); err == nil {
			if real := ParseResolvConf(string(data)); len(real) > 0 {
				return real
			}
		}
	}
	return list
}

// ParseResolvConf: адреса из строк nameserver.
func ParseResolvConf(out string) []string {
	var list []string
	for _, line := range strings.Split(out, "\n") {
		if f := strings.Fields(line); len(f) >= 2 && f[0] == "nameserver" {
			list = append(list, f[1])
		}
	}
	return list
}

// nmcliVPN: активные VPN и WireGuard из NetworkManager; на сервере без него - пусто.
func nmcliVPN() []NetConn {
	if !runner.Has("nmcli") {
		return nil
	}
	out, _ := runner.Output("nmcli", "-t", "-f", "NAME,TYPE", "connection", "show", "--active")
	var list []NetConn
	for _, line := range strings.Split(out, "\n") {
		name, typ, ok := strings.Cut(line, ":")
		if ok && (typ == "vpn" || typ == "wireguard") {
			list = append(list, NetConn{Name: name, State: "Connected"})
		}
	}
	return list
}

// Proxy: системный SOCKS-прокси службы macOS.
type Proxy struct {
	Enabled bool
	Server  string
	Port    string
}

// ParseSocksProxy разбирает networksetup -getsocksfirewallproxy.
func ParseSocksProxy(out string) Proxy {
	var p Proxy
	for _, line := range strings.Split(out, "\n") {
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		v = strings.TrimSpace(v)
		switch strings.TrimSpace(k) {
		case "Enabled":
			p.Enabled = v == "Yes"
		case "Server":
			p.Server = v
		case "Port":
			p.Port = v
		}
	}
	return p
}

func SocksProxy(service string) Proxy {
	out, _ := runner.Output("networksetup", "-getsocksfirewallproxy", service)
	return ParseSocksProxy(out)
}
