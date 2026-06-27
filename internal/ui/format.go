package ui

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
)

// Форматирование чисел и дат одинаково во всех командах.

// Bytes: размер по-человечески - знак после запятой только у чисел меньше десяти: "4.2 GB", "37 GB".
func Bytes(n int64) string {
	units := []struct {
		size int64
		name string
	}{{1 << 40, "TB"}, {1 << 30, "GB"}, {1 << 20, "MB"}, {1 << 10, "KB"}}
	for _, u := range units {
		if n < u.size {
			continue
		}
		v := float64(n) / float64(u.size)
		if v < 10 {
			return fmt.Sprintf("%.1f %s", v, u.name)
		}
		return fmt.Sprintf("%.0f %s", v, u.name)
	}
	return fmt.Sprintf("%d B", n)
}

// Short: длительность одним числом для колонок - "40 с", "5 мин", "30 ч", "12 дн", "2 мес".
func Short(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%d с", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%d мин", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%d ч", int(d.Hours()))
	case d < 60*24*time.Hour:
		return fmt.Sprintf("%d дн", int(d.Hours()/24))
	}
	return fmt.Sprintf("%d мес", int(d.Hours()/24/30))
}

// Ago: давность для подписи - "только что", "5 мин назад", "3 дн назад"; нулевое время - "только что".
func Ago(t time.Time) string {
	d := time.Since(t)
	if t.IsZero() || d < time.Minute {
		return "только что"
	}
	return Short(d) + " назад"
}

// Count: число со словом в нужной форме - "1 проект", "3 проекта", "11 проектов".
func Count(n int, one, few, many string) string {
	return strconv.Itoa(n) + " " + Plural(n, one, few, many)
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
	if width <= 0 {
		return ""
	}
	if ansi.StringWidth(s) <= width {
		return s
	}
	return ansi.Truncate(s, width, "~")
}

// Fit: ровно width колонок - короткое добивается пробелами, длинное обрезается с тильдой.
func Fit(s string, width int) string { return Pad(Clip(s, width), width) }

// Width: ширина строки в колонках терминала - без кодов цвета, широкие символы за два.
func Width(s string) int { return ansi.StringWidth(s) }

// Dash: прочерк вместо пустого значения.
func Dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// DaysLeft: дата и сколько до неё осталось; близкий срок жёлтым, прошедший красным.
func DaysLeft(t time.Time) string {
	d := DaysUntil(t, time.Now())
	date := t.Format("2006-01-02")
	switch {
	case d <= 0:
		return Bad(date + ", " + Expired(d))
	case d < 14:
		return Warn(fmt.Sprintf("%s, осталось %d дн", date, d))
	}
	return fmt.Sprintf("%s, осталось %d дн", date, d)
}

// DaysUntil: сколько дней до t на момент now, с округлением вверх: 12 часов - это 1 день.
// Ноль и меньше - срок прошёл: 0 - меньше суток назад, -N - N полных дней назад.
func DaysUntil(t, now time.Time) int {
	const day = 24 * time.Hour
	if d := t.Sub(now); d > 0 {
		return int((d + day - 1) / day)
	}
	return -int(now.Sub(t) / day)
}

// Expired: "истёк сегодня" или "истёк N дн назад" для DaysUntil <= 0.
func Expired(days int) string {
	if days == 0 {
		return "истёк сегодня"
	}
	return fmt.Sprintf("истёк %d дн назад", -days)
}

// Terminal: stderr смотрит в терминал - туда можно рисовать перезаписываемую строку хода.
func Terminal() bool {
	st, err := os.Stderr.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}

// Bar: полоска загрузки в фирменных цветах: спокойно, внимание, плохо.
func Bar(pct, width int) string {
	pct = max(0, min(100, pct))
	full := pct * width / 100
	head := strings.Repeat("\u2588", full)
	var b string
	switch {
	case pct >= 85:
		b = Bad(head) + Dim(strings.Repeat("\u2591", width-full))
	case pct >= 60:
		b = Warn(head) + Dim(strings.Repeat("\u2591", width-full))
	default:
		b = OK(head) + Dim(strings.Repeat("\u2591", width-full))
	}
	return b + " " + Pad(strconv.Itoa(pct)+"%", 5)
}
