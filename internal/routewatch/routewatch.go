// Package routewatch: решает, какие маршруты обхода VPN сломаны, и ведёт журнал починок.
package routewatch

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/TimurTurdyev/tim/internal/netinfo"
	"github.com/TimurTurdyev/tim/internal/runner"
)

// Repair: маршрут, который надо переставить, и почему.
type Repair struct {
	Dest   string    `json:"dest"`
	Reason string    `json:"reason"`
	At     time.Time `json:"at"`
}

// Decide сравнивает нужные маршруты с таблицей. Сломан тот, которого нет, который ушёл в туннель
// или который смотрит на шлюз прошлой сети: интерфейс при этом правильный, и по одному
// интерфейсу поломку не видно.
func Decide(want []string, table []netinfo.TableRoute, gw, iface string) []Repair {
	have := map[string]netinfo.TableRoute{}
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
		r, ok := have[d]
		switch {
		case !ok:
			out = append(out, Repair{Dest: d, Reason: "нет маршрута"})
		case netinfo.IsTunnelIface(r.Iface):
			out = append(out, Repair{Dest: d, Reason: "ушёл в туннель " + r.Iface})
		case r.Gateway != gw:
			out = append(out, Repair{Dest: d, Reason: "шлюз " + r.Gateway + " вместо " + gw})
		case iface != "" && r.Iface != iface:
			out = append(out, Repair{Dest: d, Reason: "интерфейс " + r.Iface + " вместо " + iface})
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
	return s, true
}

// Summary: причины починок, сложенные по смыслу, для статистики.
func Summary(list []Repair) map[string]int {
	m := map[string]int{}
	for _, r := range list {
		kind := r.Reason
		switch {
		case strings.HasPrefix(kind, "шлюз"):
			kind = "сменилась сеть"
		case strings.HasPrefix(kind, "ушёл в туннель"):
			kind = "VPN забрал маршрут"
		}
		m[kind]++
	}
	return m
}
