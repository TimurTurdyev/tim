package health

import (
	"fmt"
	"time"

	"github.com/TimurTurdyev/tim/internal/brand"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// Виды беды на машине - по ним главный экран выбирает, куда вести по enter.
const (
	Bypass Trouble = iota
	DiskFull
	Memory
	Guard
)

// memTight: доступно памяти меньше стольких процентов - системе тесно.
const memTight = 15

// Trouble: вид беды.
type Trouble int

// Warning: беда словами и её вид.
type Warning struct {
	Kind Trouble
	Text string
}

// Machine: признаки беды на машине, по которым маскот выбирает настроение, а сводка и главный экран -
// предупреждения. Правило одно, чтобы маскот и предупреждение не спорили.
type Machine struct {
	DiskPct             int
	DiskFree            int64
	BypassOK, BypassAll int // BypassAll 0 - обход не считали
	MemFree             int // доступно памяти в процентах по оценке системы; 0 - не знаем
	GuardMissing        bool
}

func (m Machine) memTight() bool { return m.MemFree > 0 && m.MemFree < memTight }

// bypassBroken: сколько целей обхода ушло в VPN; much - больше четверти, это уже не пара адресов.
func (m Machine) bypassBroken() (some, much bool) {
	broken := m.BypassAll - m.BypassOK
	return m.BypassAll > 0 && broken > 0, m.BypassAll > 0 && broken*4 > m.BypassAll
}

// Warnings: беды словами, худшие первыми.
func (m Machine) Warnings() []Warning {
	var out []Warning
	if some, _ := m.bypassBroken(); some {
		out = append(out, Warning{Bypass, ui.Count(m.BypassAll-m.BypassOK, "адрес", "адреса", "адресов") + " через VPN вместо обхода"})
	}
	if m.DiskPct >= 85 {
		out = append(out, Warning{DiskFull, fmt.Sprintf("диск занят на %d%%, свободно %s", m.DiskPct, ui.Bytes(m.DiskFree))})
	}
	if m.memTight() {
		out = append(out, Warning{Memory, fmt.Sprintf("памяти мало: системе доступно %d%%", m.MemFree)})
	}
	if m.GuardMissing {
		out = append(out, Warning{Guard, "страж маршрутов не установлен: после смены сети обход сломается"})
	}
	return out
}

// Mood: ошибка - диск больше 95% или в VPN ушла заметная часть целей обхода; тревога - диск от 85%,
// памяти мало, страж не работает, пара адресов мимо обхода; ночью без беды спит.
func (m Machine) Mood(now time.Time) brand.Mood {
	some, much := m.bypassBroken()
	switch {
	case m.DiskPct > 95 || much:
		return brand.Upset
	case m.DiskPct >= 85 || m.memTight() || m.GuardMissing || some:
		return brand.Worried
	case now.Hour() < 6:
		return brand.Asleep
	}
	return brand.Calm
}
