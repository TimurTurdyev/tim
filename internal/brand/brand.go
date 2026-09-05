// Package brand: фирменные цвета, логотип и маскот tim. Без внешних зависимостей:
// пакет нужен на старте каждой команды.
package brand

import (
	"fmt"
	"strings"
)

// Роли цветов. Палитра взята у маскота: бирюзовое худи, зелёное свечение экрана, синий конец
// градиента логотипа; остальное - каркас экранов.
const (
	Teal Color = iota
	Green
	Blue
	Amber
	Red
	Gray
	Shade    // фон выделенной строки
	Text     // основной текст экранов
	Crumb    // крошки в верхней строке
	Bar      // фон верхней и нижней строк
	KeyCap   // подложка клавиш
	Ghost    // пустая часть полосы
	OnAccent // текст на бирюзовом
	OnBad    // текст на красном
	colors
)

// Dark - для тёмного терминала. Light - для светлого: те же цвета темнее, выделение светло-бирюзовое.
var (
	Dark = Palette{
		Teal: {40, 170, 160}, Green: {60, 230, 140}, Blue: {90, 140, 230}, Amber: {235, 175, 60},
		Red: {230, 70, 70}, Gray: {125, 130, 140}, Shade: {26, 58, 56}, Text: {215, 218, 224},
		Crumb: {215, 218, 224}, Bar: {31, 34, 39}, KeyCap: {51, 55, 63}, Ghost: {43, 47, 55},
		OnAccent: {14, 26, 25}, OnBad: {255, 255, 255},
	}
	Light = Palette{
		Teal: {21, 122, 114}, Green: {26, 154, 85}, Blue: {45, 95, 184}, Amber: {181, 122, 12},
		Red: {201, 48, 44}, Gray: {107, 114, 128}, Shade: {214, 239, 235}, Text: {31, 35, 40},
		Crumb: {47, 51, 56}, Bar: {236, 239, 243}, KeyCap: {223, 227, 234}, Ghost: {223, 227, 232},
		OnAccent: {255, 255, 255}, OnBad: {255, 255, 255},
	}
)

// Title: слово tim крупными буквами, по строке на цвет градиента.
var Title = []string{
	"\u2588\u2580\u2588\u2580\u2588 \u2588 \u2588\u2580\u2584\u2580\u2588",
	"  \u2588   \u2588 \u2588 \u2580 \u2588",
	"  \u2580   \u2580 \u2580   \u2580",
}

// TitleGradient: зелёный экран - бирюзовое худи - синий.
var TitleGradient = []Color{Green, Teal, Blue}

// Color: роль цвета в палитре.
type Color int

// Palette: цвет каждой роли.
type Palette [colors]RGB

// RGB: цвет палитры.
type RGB struct{ R, G, B uint8 }

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
