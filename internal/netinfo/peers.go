package netinfo

import (
	"runtime"
	"strings"

	"github.com/TimurTurdyev/tim/internal/runner"
)

// Peer: открытое соединение - какой процесс, с какого своего адреса и куда.
type Peer struct {
	Proc, Local, Remote string
}

// ParseLsofPeers разбирает lsof -nP -i -w: строки вида
// "firefox 123 user 75u IPv4 0x.. 0t0 TCP 10.8.1.1:53261->34.107.243.93:443 (ESTABLISHED)".
// Слушающие сокеты и UDP без адресата пропускаются.
func ParseLsofPeers(out string) []Peer {
	var list []Peer
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) < 9 {
			continue
		}
		conn := f[8]
		local, remote, ok := strings.Cut(conn, "->")
		if !ok {
			continue
		}
		list = append(list, Peer{Proc: f[0], Local: hostOf(local), Remote: hostOf(remote)})
	}
	return list
}

// ParseSSPeers разбирает ss -tunpH: "tcp ESTAB 0 0 10.8.0.2:52345 82.1.1.1:443 users:(("chrome",pid=1,fd=5))".
func ParseSSPeers(out string) []Peer {
	var list []Peer
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) < 6 || f[1] == "LISTEN" || f[1] == "UNCONN" {
			continue
		}
		p := Peer{Local: hostOf(f[4]), Remote: hostOf(f[5])}
		if i := strings.Index(line, `users:(("`); i >= 0 {
			rest := line[i+9:]
			if j := strings.Index(rest, `"`); j > 0 {
				p.Proc = rest[:j]
			}
		}
		list = append(list, p)
	}
	return list
}

// hostOf: адрес без порта; IPv6 в lsof и ss идёт в квадратных скобках.
func hostOf(addr string) string {
	if strings.HasPrefix(addr, "[") {
		if i := strings.Index(addr, "]"); i > 0 {
			return addr[1:i]
		}
	}
	if i := strings.LastIndex(addr, ":"); i >= 0 {
		return addr[:i]
	}
	return addr
}

// Peers: открытые соединения сейчас.
func Peers() []Peer {
	if runtime.GOOS == "linux" {
		out, _ := runner.Output("ss", "-tunpH")
		return ParseSSPeers(out)
	}
	out, _ := runner.Output("lsof", "-nP", "-i", "-w")
	return ParseLsofPeers(out)
}
