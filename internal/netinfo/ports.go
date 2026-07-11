package netinfo

import (
	"net"
	"regexp"
	"runtime"
	"strconv"
	"strings"

	"github.com/TimurTurdyev/tim/internal/runner"
)

// DefaultPort: с него раздаются порты dev-серверов.
const DefaultPort = 8000

// ssProcRe: имя и pid процесса в колонке users у ss.
var ssProcRe = regexp.MustCompile(`\(\("([^"]+)",pid=(\d+)`)

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

// FirstFree: первый порт от from до 65535, который не занят; ok=false - свободных не осталось.
func FirstFree(from int, taken func(int) bool) (port int, ok bool) {
	for p := max(from, 1); p <= 65535; p++ {
		if !taken(p) {
			return p, true
		}
	}
	return 0, false
}

// Port: номер порта строкой, как его пишут в адресе.
func Port(n int) string { return strconv.Itoa(n) }

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
