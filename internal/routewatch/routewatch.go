// Package routewatch: решает, какие маршруты обхода VPN сломаны, и ведёт журнал починок.
package routewatch

import (
	"encoding/json"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/TimurTurdyev/tim/internal/netinfo"
	"github.com/TimurTurdyev/tim/internal/runner"
)

// Kind: почему маршрут пришлось чинить.
type Kind string

const (
	Missing Kind = "missing" // маршрута нет
	Tunnel  Kind = "tunnel"  // ушёл в туннель
	Gateway Kind = "gateway" // смотрит на шлюз прошлой сети
	Iface   Kind = "iface"   // не тот интерфейс
)

// Repair: маршрут, который надо переставить. Reason - подробность для журнала: что было вместо чего.
type Repair struct {
	Dest   string    `json:"dest"`
	Kind   Kind      `json:"kind"`
	Reason string    `json:"reason"`
	At     time.Time `json:"at"`
}

// Decide сравнивает нужные маршруты с таблицей. Сломан тот, которого нет, который ушёл в туннель
// или который смотрит на шлюз прошлой сети: интерфейс при этом правильный, и по одному
// интерфейсу поломку не видно.
func Decide(want []string, table []netinfo.TableRoute, gw, iface string) []Repair {
	have := map[netip.Prefix]netinfo.TableRoute{}
	for _, r := range table {
		have[r.Dest] = r
	}
	var out []Repair
	seen := map[string]bool{}
	for _, d := range want {
		if seen[d] {
			continue
		}
		seen[d] = true
		p, _ := netinfo.ParseDest(d)
		r, ok := have[p]
		switch {
		case !ok:
			out = append(out, Repair{Dest: d, Kind: Missing, Reason: "нет маршрута"})
		case netinfo.IsTunnelIface(r.Iface):
			out = append(out, Repair{Dest: d, Kind: Tunnel, Reason: "ушёл в туннель " + r.Iface})
		case r.Gateway != gw:
			out = append(out, Repair{Dest: d, Kind: Gateway, Reason: "шлюз " + r.Gateway + " вместо " + gw})
		case iface != "" && r.Iface != iface:
			out = append(out, Repair{Dest: d, Kind: Iface, Reason: "интерфейс " + r.Iface + " вместо " + iface})
		}
	}
	return out
}

// Status: что страж знает о себе; пишется каждый цикл, читается командой tim route status.
type Status struct {
	PID       int       `json:"pid"`
	Started   time.Time `json:"started"`
	LastCheck time.Time `json:"last_check"`
	Gateway   string    `json:"gateway"`
	Iface     string    `json:"iface"`
	Want      int       `json:"want"`
	OK        int       `json:"ok"`
	Error     string    `json:"error,omitempty"`
	Repairs   []Repair  `json:"repairs"`
}

// KeepRepairs: сколько последних починок помнить.
const KeepRepairs = 200

// AddRepairs дописывает починки и обрезает журнал.
func (s *Status) AddRepairs(list []Repair) {
	s.Repairs = append(s.Repairs, list...)
	if len(s.Repairs) > KeepRepairs {
		s.Repairs = s.Repairs[len(s.Repairs)-KeepRepairs:]
	}
}

// Since: сколько починок после момента.
func (s Status) Since(t time.Time) int {
	n := 0
	for _, r := range s.Repairs {
		if r.At.After(t) {
			n++
		}
	}
	return n
}

// Path: файл состояния в каталоге стража.
func Path(dir string) string { return filepath.Join(dir, "routes-status.json") }

// Save пишет состояние атомарно и с правом чтения для всех: страж под root, а читает пользователь.
func (s Status) Save(dir string) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return runner.WriteAtomic(Path(dir), data, 0o644)
}

// Load читает состояние; ok=false, если страж ни разу не работал.
func Load(dir string) (Status, bool) {
	data, err := os.ReadFile(Path(dir))
	if err != nil {
		return Status{}, false
	}
	var s Status
	if json.Unmarshal(data, &s) != nil {
		return Status{}, false
	}
	for i, r := range s.Repairs {
		if r.Kind == "" {
			s.Repairs[i].Kind = kindOf(r.Reason)
		}
	}
	return s, true
}

// kindOf: у записей прошлых версий был только текст причины.
func kindOf(reason string) Kind {
	switch {
	case strings.HasPrefix(reason, "шлюз"):
		return Gateway
	case strings.HasPrefix(reason, "ушёл в туннель"):
		return Tunnel
	case strings.HasPrefix(reason, "интерфейс"):
		return Iface
	}
	return Missing
}

// String: причина по смыслу, для статистики.
func (k Kind) String() string {
	switch k {
	case Gateway:
		return "сменилась сеть"
	case Tunnel:
		return "VPN забрал маршрут"
	case Iface:
		return "не тот интерфейс"
	}
	return "нет маршрута"
}

// Summary: сколько починок каждого вида.
func Summary(list []Repair) map[Kind]int {
	m := map[Kind]int{}
	for _, r := range list {
		m[r.Kind]++
	}
	return m
}
