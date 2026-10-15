package motd

import (
	"time"

	"github.com/TimurTurdyev/tim/internal/brand"
)

// Health: признаки беды, по которым маскот выбирает настроение. Главный экран заполняет их из своих
// плиток, сводка - из своих разделов; правило одно, чтобы маскот и предупреждение не спорили.
type Health struct {
	DiskPct             int
	BypassOK, BypassAll int // BypassAll 0 - обход не считали
	SwapShort           bool
	GuardMissing        bool
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
