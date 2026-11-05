// Package level: общая шкала "насколько всё плохо" и её цвета в терминале. Сама шкала живёт в report,
// чтобы её видели и другие модули; здесь - псевдонимы для разделов и раскраска.
package level

import (
	"github.com/TimurTurdyev/tim/internal/ui"
	"github.com/TimurTurdyev/tim/report"
)

const (
	Skip = report.Skip
	Info = report.Info
	OK   = report.OK
	Warn = report.Warn
	Bad  = report.Bad
)

// Level: то же, что report.Level.
type Level = report.Level

// Worst: худший из уровней, Skip без уровней.
func Worst(levels ...Level) Level { return report.Worst(levels...) }

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
