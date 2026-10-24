// Package level: общая шкала "насколько всё плохо" для проверок, находок и сообщений экранов.
package level

import (
	"fmt"

	"github.com/TimurTurdyev/tim/internal/ui"
)

// Level: порядок важен, худший уровень - наибольший.
type Level int

const (
	Skip Level = iota // проверить нечем, например у проекта нет url
	Info              // к сведению: не плохо и не хорошо
	OK
	Warn
	Bad
)

func (l Level) String() string {
	switch l {
	case Bad:
		return "плохо"
	case Warn:
		return "внимание"
	case OK:
		return "ок"
	}
	return "-"
}

// MarshalText: в --json уровень словом, а не числом.
func (l Level) MarshalText() ([]byte, error) {
	return []byte([...]string{"skip", "info", "ok", "warn", "bad"}[l]), nil
}

// UnmarshalText: уровень словом обратно - кэш проверок читается тем же типом, что пишется.
func (l *Level) UnmarshalText(b []byte) error {
	for i, w := range [...]string{"skip", "info", "ok", "warn", "bad"} {
		if string(b) == w {
			*l = Level(i)
			return nil
		}
	}
	return fmt.Errorf("неизвестный уровень %q", b)
}

// Worst: худший из уровней, Skip без уровней.
func Worst(levels ...Level) Level {
	worst := Skip
	for _, l := range levels {
		worst = max(worst, l)
	}
	return worst
}

// Lamp: строка со значком уровня - читается и без цвета.
func Lamp(l Level, text string) string {
	switch l {
	case Bad:
		return ui.Bad("\u2715 " + text)
	case Warn:
		return ui.Warn("\u25b2 " + text)
	case OK:
		return ui.OK("\u25cf " + text)
	}
	return ui.Dim("- " + text)
}

// Paint: текст цветом уровня, без значка - для ячеек таблиц и строк прогноза.
func Paint(l Level, text string) string {
	switch l {
	case Bad:
		return ui.Bad(text)
	case Warn:
		return ui.Warn(text)
	}
	return text
}
