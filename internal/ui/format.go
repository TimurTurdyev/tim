package ui

import (
	"fmt"
	"os"
	"time"
)

// Форматирование чисел и дат одинаково во всех командах.

// Bytes: размер по-человечески, с одним знаком после запятой.
func Bytes(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0f KB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}

// Duration: длительность словами - "3 дн", "2 ч 15 мин", "40 с".
func Duration(d time.Duration) string {
	switch {
	case d >= 48*time.Hour:
		return fmt.Sprintf("%d дн", int(d.Hours()/24))
	case d >= time.Hour:
		return fmt.Sprintf("%d ч %d мин", int(d.Hours()), int(d.Minutes())%60)
	case d >= time.Minute:
		return fmt.Sprintf("%d мин", int(d.Minutes()))
	}
	return fmt.Sprintf("%d с", int(d.Seconds()))
}

// Clip обрезает строку до width рун, помечая обрез тильдой.
func Clip(s string, width int) string {
	r := []rune(s)
	if len(r) <= width {
		return s
	}
	return string(r[:width-1]) + "~"
}

// Dash: прочерк вместо пустого значения.
func Dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// DaysLeft: дата и сколько до неё осталось; близкий срок жёлтым, прошедший красным.
func DaysLeft(t time.Time) string {
	d := int(time.Until(t).Hours() / 24)
	s := fmt.Sprintf("%s, осталось %d дн", t.Format("2006-01-02"), d)
	switch {
	case d < 0:
		return Bad(fmt.Sprintf("%s, истёк %d дн назад", t.Format("2006-01-02"), -d))
	case d < 14:
		return Warn(s)
	}
	return s
}

// Terminal: stderr смотрит в терминал - туда можно рисовать перезаписываемую строку хода.
func Terminal() bool {
	st, err := os.Stderr.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}
