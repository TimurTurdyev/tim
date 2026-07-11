package brand

import "strings"

// Енот - маскот-заглушка экранного режима: 24x12 пикселей четвертинками клетки, 12 колонок на 6 строк.
// В клетке две краски: одна символом, другая фоном; прозрачное закрашивается второй, если она есть.

var raccoonPal = map[byte]RGB{
	'G': {150, 150, 160}, // шерсть
	'D': {60, 60, 70},    // маска, полосы, лапы
	'L': {220, 220, 225}, // светлая морда
	'K': {40, 36, 44},    // глаза и нос
	'Z': {140, 170, 255}, // z во сне
	'R': {230, 60, 60},   // глаза при ошибке
}

var raccoonBase = []string{
	"....GG.....GG...........",
	"....GGGGGGGGG...........",
	"....GLDDGDDLG...........",
	"....GLDKGKDLG...........",
	"....GGLLLLLGG..GDGDGDG..",
	".....GGLKLGG..GDGDGDGD..",
	"....GGGGGGGGGGGDGDGDG...",
	"...GGGGGGGGGGGGGDGD.....",
	"...GGGGGGGGGGGGG........",
	"...GG..GG....GG..GG.....",
	"...DD..DD....DD..DD.....",
	"........................",
}

// Четвертинки по битам: левый верх, правый верх, левый низ, правый низ.
const quads = " \u2597\u2596\u2584\u259d\u2590\u259e\u259f\u2598\u259a\u258c\u2599\u2580\u259c\u259b\u2588"

// Raccoon: картинка енота для терминала; как Mascot, без 256 цветов - пусто.
func Raccoon(m Mood, d Depth) []string {
	if d == Basic {
		return nil
	}
	g := make([][]byte, len(raccoonBase))
	for i, r := range raccoonBase {
		g[i] = []byte(r)
	}
	switch m {
	case Upset:
		g[3][7], g[3][9] = 'R', 'R'
	case Asleep:
		g[3][7], g[3][9] = 'D', 'D'
		g[0][21], g[1][22] = 'Z', 'Z'
	case Blink:
		g[3][7], g[3][9] = 'D', 'D'
	}
	var lines []string
	for y := 0; y+1 < len(g); y += 2 {
		var b strings.Builder
		for x := 0; x+1 < len(g[y]); x += 2 {
			px := [4]byte{g[y][x], g[y][x+1], g[y+1][x], g[y+1][x+1]}
			var fg, bg byte
			count := map[byte]int{}
			for _, c := range px {
				if c != '.' {
					count[c]++
				}
			}
			for c, n := range count {
				switch {
				case fg == 0 || n > count[fg] || (n == count[fg] && c < fg):
					bg, fg = fg, c
				case bg == 0 || n > count[bg] || (n == count[bg] && c < bg):
					bg = c
				}
			}
			if fg == 0 {
				b.WriteString("\x1b[0m ")
				continue
			}
			bits := 0
			for i, c := range px {
				if c == fg {
					bits |= 8 >> i
				}
			}
			if bg == 0 {
				b.WriteString("\x1b[0m\x1b[" + raccoonPal[fg].FG(d, "") + "m" + string([]rune(quads)[bits]))
			} else {
				b.WriteString("\x1b[" + raccoonPal[fg].FG(d, "") + ";" + raccoonPal[bg].BG(d) + "m" + string([]rune(quads)[bits]))
			}
		}
		b.WriteString("\x1b[0m")
		lines = append(lines, b.String())
	}
	return lines
}
