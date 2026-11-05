package netinfo

import (
	"net"
	"net/netip"
	"regexp"
	"runtime"
	"slices"
	"strings"

	"github.com/TimurTurdyev/tim/internal/runner"
)

// DefaultPort: с него раздаются порты dev-серверов.
const DefaultPort = 8000

// ssProcRe: имя и pid первого процесса в колонке users у ss, ssPidRe - pid всех.
var (
	ssProcRe = regexp.MustCompile(`\(\("([^"]+)",pid=(\d+)`)
	ssPidRe  = regexp.MustCompile(`,pid=(\d+),`)
)

// Ports: снимок слушающих портов - один lsof или ss на перебор, а не на каждую пробу.
type Ports map[string]Listener

// BusyPorts снимает, кто сейчас слушает.
func BusyPorts() Ports {
	ports := Ports{}
	for _, l := range Listening() {
		ports[l.Port] = l
	}
	return ports
}

// Who: кто слушает порт. Если снимок его не знает (чужой процесс без прав, нет lsof или ss),
// порт пробуется занять: занятый не даст, и тогда он занят неизвестно кем.
func (p Ports) Who(port string) (Listener, bool) {
	if l, ok := p[port]; ok {
		return l, true
	}
	ln, err := net.Listen("tcp", ":"+port)
	if err != nil {
		return Listener{Port: port, Command: "?"}, true
	}
	_ = ln.Close()
	return Listener{}, false
}

// Busy: порт занят.
func (p Ports) Busy(port string) bool {
	_, busy := p.Who(port)
	return busy
}

// Listening: слушающие TCP-порты через lsof.
type Listener struct {
	Port string
	PID  string
	// PIDs: все, кто держит сокет; у php -S с воркерами это мастер и каждый воркер.
	PIDs    []string
	Command string
	// Network: слушает не только эту машину - все адреса или адрес в сети; такой dev-сервер виден
	// всем в той же Wi-Fi, в кафе и в офисе.
	Network bool
}

// exposed: адрес привязки виден из сети - всё, кроме loopback.
func exposed(addr string) bool {
	addr = strings.Trim(addr, "[]")
	if addr == "*" || addr == "" {
		return true
	}
	if a, err := netip.ParseAddr(strings.Split(addr, "%")[0]); err == nil {
		return !a.IsLoopback()
	}
	return addr != "localhost"
}

func ParseLsof(out string) []Listener {
	byPort := map[string]int{}
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
		cut := strings.LastIndex(name, ":")
		port, open := name[cut+1:], exposed(name[:cut])
		j, seen := byPort[port]
		if !seen {
			j, byPort[port] = len(list), len(list)
			list = append(list, Listener{Port: port, PID: f[1], Command: f[0]})
		}
		list[j].Network = list[j].Network || open
		list[j].addPID(f[1])
	}
	return list
}

func (l *Listener) addPID(pid string) {
	if !slices.Contains(l.PIDs, pid) {
		l.PIDs = append(l.PIDs, pid)
	}
}

// ParseSS: ss -Hltnp -> слушающие порты; один порт на IPv4 и IPv6 - одной строкой.
func ParseSS(out string) []Listener {
	byPort := map[string]int{}
	var list []Listener
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) < 4 {
			continue
		}
		local := f[3]
		cut := strings.LastIndex(local, ":")
		port, open := local[cut+1:], exposed(local[:cut])
		if port == "" {
			continue
		}
		j, seen := byPort[port]
		if !seen {
			j, byPort[port] = len(list), len(list)
			list = append(list, Listener{Port: port})
		}
		l := &list[j]
		l.Network = l.Network || open
		if m := ssProcRe.FindStringSubmatch(line); m != nil && l.PID == "" {
			l.Command, l.PID = m[1], m[2]
		}
		for _, m := range ssPidRe.FindAllStringSubmatch(line, -1) {
			l.addPID(m[1])
		}
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

// PortBusy: кто слушает порт - для одной проверки; для перебора - BusyPorts.
func PortBusy(port string) (Listener, bool) { return BusyPorts().Who(port) }
