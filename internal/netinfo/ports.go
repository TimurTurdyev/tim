package netinfo

import (
	"net"
	"strconv"
)

// DefaultPort: с него раздаются порты dev-серверов.
const DefaultPort = 8000

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
