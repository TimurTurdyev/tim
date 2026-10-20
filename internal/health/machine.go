package health

import (
	"fmt"
	"time"

	"github.com/TimurTurdyev/tim/internal/brand"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// Trouble: вид беды на машине - по нему главный экран выбирает, куда вести по enter.
type Trouble int

const (
	Bypass Trouble = iota
	DiskFull
	Swap
	Guard
)

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
	SwapShort           bool
	GuardMissing        bool
}

// Warnings: беды словами, худшие первыми.
func (m Machine) Warnings() []Warning {
	var out []Warning
	if m.BypassAll > 0 && m.BypassOK < m.BypassAll {
		out = append(out, Warning{Bypass, ui.Count(m.BypassAll-m.BypassOK, "адрес", "адреса", "адресов") + " через VPN вместо обхода"})
	}
	if m.DiskPct >= 85 {
		out = append(out, Warning{DiskFull, fmt.Sprintf("диск занят на %d%%, свободно %s", m.DiskPct, ui.Bytes(m.DiskFree))})
	}
	if m.SwapShort {
		out = append(out, Warning{Swap, "памяти не хватает: своп занят больше чем наполовину"})
	}
	if m.GuardMissing {
		out = append(out, Warning{Guard, "страж маршрутов не установлен: после смены сети обход сломается"})
	}
	return out
}

// Mood: ошибка - диск больше 95% или цели обхода ушли в VPN; тревога - диск от 85%, своп занят больше
// половины, страж маршрутов не работает; ночью без беды спит.
func (m Machine) Mood(now time.Time) brand.Mood {
	switch {
	case m.DiskPct > 95 || m.BypassOK < m.BypassAll:
		return brand.Upset
	case m.DiskPct >= 85 || m.SwapShort || m.GuardMissing:
		return brand.Worried
	case now.Hour() < 6:
		return brand.Asleep
	}
	return brand.Calm
}
