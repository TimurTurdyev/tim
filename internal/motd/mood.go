package motd

import (
	"fmt"
	"time"

	"github.com/TimurTurdyev/tim/internal/brand"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// Trouble: вид беды - по нему главный экран выбирает, куда вести по enter.
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

// Health: признаки беды, по которым маскот выбирает настроение. Главный экран заполняет их из своих
// плиток, сводка - из своих разделов; правило одно, чтобы маскот и предупреждение не спорили.
type Health struct {
	DiskPct             int
	DiskFree            int64
	BypassOK, BypassAll int // BypassAll 0 - обход не считали
	SwapShort           bool
	GuardMissing        bool
}

// Warnings: беды словами, худшие первыми - одни и те же для сводки и главного экрана.
func (h Health) Warnings() []Warning {
	var out []Warning
	if h.BypassAll > 0 && h.BypassOK < h.BypassAll {
		out = append(out, Warning{Bypass, ui.Count(h.BypassAll-h.BypassOK, "адрес", "адреса", "адресов") + " через VPN вместо обхода"})
	}
	if h.DiskPct >= 85 {
		out = append(out, Warning{DiskFull, fmt.Sprintf("диск занят на %d%%, свободно %s", h.DiskPct, ui.Bytes(h.DiskFree))})
	}
	if h.SwapShort {
		out = append(out, Warning{Swap, "памяти не хватает: своп занят больше чем наполовину"})
	}
	if h.GuardMissing {
		out = append(out, Warning{Guard, "страж маршрутов не установлен: после смены сети обход сломается"})
	}
	return out
}

// Mood: ошибка - диск больше 95% или цели обхода ушли в VPN; тревога - диск от 85%, своп занят больше
// половины, страж маршрутов не работает; ночью без беды спит.
func (h Health) Mood(now time.Time) brand.Mood {
	switch {
	case h.DiskPct > 95 || h.BypassOK < h.BypassAll:
		return brand.Upset
	case h.DiskPct >= 85 || h.SwapShort || h.GuardMissing:
		return brand.Worried
	case now.Hour() < 6:
		return brand.Asleep
	}
	return brand.Calm
}
