package motd

import (
	"time"

	"github.com/TimurTurdyev/tim/internal/brand"
)

// Health: признаки беды, по которым маскот выбирает настроение. Главный экран заполняет их из своих
// плиток, сводка - из своих разделов; правило одно, чтобы маскот и предупреждение не спорили.
type Health struct {
	DiskPct, MemPct     int
	BypassOK, BypassAll int // BypassAll 0 - обход не считали
	GuardMissing        bool
}

// Mood: ошибка - диск больше 95% или цели обхода ушли в VPN; тревога - диск от 85%, память от 90%,
// страж маршрутов не работает; ночью без беды спит.
func (h Health) Mood(now time.Time) brand.Mood {
	switch {
	case h.DiskPct > 95 || h.BypassOK < h.BypassAll:
		return brand.Upset
	case h.DiskPct >= 85 || h.MemPct >= 90 || h.GuardMissing:
		return brand.Worried
	case now.Hour() < 6:
		return brand.Asleep
	}
	return brand.Calm
}
