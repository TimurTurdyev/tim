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

// Warning: беда словами и её вид. Text - "<объект>: <что>"; Tail - подробность, которую на узком
// месте можно опустить.
type Warning struct {
	Kind Trouble
	Text string
	Tail string
}

// Full: беда целиком, с подробностью.
func (w Warning) Full() string {
	if w.Tail == "" {
		return w.Text
	}
	return w.Text + ", " + w.Tail
}

// Machine: признаки беды на машине, по которым маскот выбирает настроение, а сводка и главный экран -
// предупреждения. Правило одно, чтобы маскот и предупреждение не спорили.
type Machine struct {
	DiskPct             int
	DiskFree            int64
	BypassOK, BypassAll int // BypassAll 0 - обход не считали
	MemFree             int // доступно памяти в процентах по оценке системы; 0 - не знаем
	SwapUsed, SwapTotal int64
	MemHolder           string // кто больше всех держит память, если её не хватает
	GuardMissing        bool
}

// memTight: системе тесно сейчас или своп занят больше чем наполовину - памяти не хватает постоянно,
// а не изредка (то же правило, что у tim hw).
func (m Machine) memTight() bool {
	return m.MemFree > 0 && m.MemFree < memTight || m.SwapShort()
}

// MemoryShort: памяти не хватает - беда для предупреждения и строки памяти.
func (m Machine) MemoryShort() bool { return m.memTight() }

// SwapShort: своп занят больше чем наполовину.
func (m Machine) SwapShort() bool { return m.SwapTotal > 0 && m.SwapUsed*2 > m.SwapTotal }

// bypassBroken: сколько целей обхода ушло в VPN; much - больше четверти, это уже не пара адресов.
func (m Machine) bypassBroken() (some, much bool) {
	broken := m.BypassAll - m.BypassOK
	return m.BypassAll > 0 && broken > 0, m.BypassAll > 0 && broken*4 > m.BypassAll
}

// Warnings: беды словами в порядке, в котором их показывать: память, диск, обход, страж.
func (m Machine) Warnings() []Warning {
	var out []Warning
	if m.memTight() {
		w := Warning{Kind: Memory, Text: fmt.Sprintf("память: системе доступно %d%%", m.MemFree)}
		if m.SwapShort() {
			w.Text = "swap " + ui.Bytes(m.SwapUsed) + ": памяти не хватает"
		}
		if m.MemHolder != "" {
			w.Tail = "больше всех держит " + m.MemHolder
		}
		out = append(out, w)
	}
	if m.DiskPct >= 85 {
		out = append(out, Warning{Kind: DiskFull, Text: fmt.Sprintf("диск: занят на %d%%, свободно %s", m.DiskPct, ui.Bytes(m.DiskFree))})
	}
	if some, _ := m.bypassBroken(); some {
		out = append(out, Warning{Kind: Bypass, Text: "обход: " + ui.Count(m.BypassAll-m.BypassOK, "адрес", "адреса", "адресов") + " через VPN"})
	}
	if m.GuardMissing {
		out = append(out, Warning{Kind: Guard, Text: "страж: не установлен", Tail: "после смены сети обход сломается"})
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
