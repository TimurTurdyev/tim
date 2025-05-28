// Package brand: фирменные цвета, логотип и маскот tim. Без внешних зависимостей:
// пакет нужен на старте каждой команды.
package brand

import (
	"fmt"
	"strings"
)

// RGB: цвет палитры.
type RGB struct{ R, G, B uint8 }

// Палитра взята у маскота: бирюзовое худи, зелёное свечение экрана, синий конец градиента логотипа.
var (
	Teal  = RGB{40, 170, 160}
	Green = RGB{60, 230, 140}
	Blue  = RGB{90, 140, 230}
	Amber = RGB{235, 175, 60}
	Red   = RGB{230, 70, 70}
	Gray  = RGB{125, 130, 140}
	Shade = RGB{26, 58, 56} // фон выделенной строки: тёмный с бирюзой
)

// Hex для lipgloss.
func (c RGB) Hex() string { return fmt.Sprintf("#%02X%02X%02X", c.R, c.G, c.B) }

// Code256: ближайший цвет из куба 6x6x6 палитры xterm-256 - для терминалов без truecolor.
func (c RGB) Code256() int {
	step := func(v uint8) int {
		if v < 48 {
			return 0
		}
		if v < 115 {
			return 1
		}
		return (int(v) - 35) / 40
	}
	return 16 + 36*step(c.R) + 6*step(c.G) + step(c.B)
}

// Depth: сколько цветов умеет терминал.
type Depth int

const (
	Basic Depth = iota // 16 цветов
	Color256
	TrueColor
)

// DetectDepth по переменным окружения, как это делают сами терминалы.
func DetectDepth(colorterm, term string) Depth {
	switch strings.ToLower(colorterm) {
	case "truecolor", "24bit":
		return TrueColor
	}
	if strings.Contains(term, "256color") {
		return Color256
	}
	return Basic
}

// FG: последовательность цвета текста для глубины; basic - запасной код из 16 цветов.
func (c RGB) FG(d Depth, basic string) string {
	switch d {
	case TrueColor:
		return fmt.Sprintf("38;2;%d;%d;%d", c.R, c.G, c.B)
	case Color256:
		return fmt.Sprintf("38;5;%d", c.Code256())
	}
	return basic
}

// BG: то же для фона.
func (c RGB) BG(d Depth) string {
	switch d {
	case TrueColor:
		return fmt.Sprintf("48;2;%d;%d;%d", c.R, c.G, c.B)
	case Color256:
		return fmt.Sprintf("48;5;%d", c.Code256())
	}
	return ""
}

// Title: слово tim крупными буквами, по строке на цвет градиента.
var Title = []string{
	"\u2588\u2580\u2588\u2580\u2588 \u2588 \u2588\u2580\u2584\u2580\u2588",
	"  \u2588   \u2588 \u2588 \u2580 \u2588",
	"  \u2580   \u2580 \u2580   \u2580",
}

// TitleGradient: зелёный экран - бирюзовое худи - синий.
var TitleGradient = []RGB{Green, Teal, Blue}
