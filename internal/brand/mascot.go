package brand

import "strings"

// Маскот - импульсный контейнер: куб анфас с панелью-индикатором, 24x12 пикселей четвертинками клетки,
// 12 колонок на 6 строк. В клетке две краски: одна символом, другая фоном; прозрачное закрашивается
// второй, если она есть, поэтому глаза и полоса не делят клетку с корпусом.
var mascotPal = map[byte]RGB{
	'U': {156, 164, 184}, // фаска сверху
	'N': {104, 112, 131}, // корпус: светлее фона любого тёмного терминала
	'n': {70, 76, 92},    // фаска справа
	'E': {12, 22, 19},    // панель
	'k': {44, 48, 58},    // пустая полоса
	'T': {52, 196, 184},  // глаза: ярче фирменного бирюзового, на тёмной панели светятся
	'q': {24, 100, 95},   // глаза во сне
	'G': Dark[Green],     // полоса: готово
	'R': Dark[Red],       // ошибка
	'Z': {140, 170, 255}, // z во сне
}

var mascotBase = []string{
	"........................",
	"........................",
	"..UUUUUUUUUUUUUUUUUUUU..",
	"..NNNNNNNNNNNNNNNNNNnn..",
	"..NNEEEEEEEEEEEEEENNnn..",
	"..NNEEEEEEEEEEEEEENNnn..",
	"..NNEEEEEEEEEEEEEENNnn..",
	"..NNEEEEEEEEEEEEEENNnn..",
	"..NNEEEEEEEEEEEEEENNnn..",
	"..NNEEEEEEEEEEEEEENNnn..",
	"..NNkkkkkkkkkkkkkkNNnn..",
	"..NNNNNNNNNNNNNNNNNNnn..",
}

// Глаза-сегменты: какие из четырёх рядов панели горят. Ряды над ними и под ними пустые, иначе глаза
// делят клетку с корпусом или сливаются с полосой.
var mascotEyes = map[Mood][4]bool{
	Calm:   {true, true, true, true},
	Happy:  {true, true, false, false},
	Upset:  {false, false, true, true},
	Asleep: {false, false, false, true},
	Blink:  {false, false, false, true},
}

// Четвертинки по битам: левый верх, правый верх, левый низ, правый низ.
const quads = " \u2597\u2596\u2584\u259d\u2590\u259e\u259f\u2598\u259a\u258c\u2599\u2580\u259c\u259b\u2588"

// MascotWidth: ширина картинки в колонках терминала.
const MascotWidth = 12

// Mood: выражение лица.
type Mood int

const (
	Calm Mood = iota
	Happy
	Upset
	Asleep
	Blink // кадр моргания: глаза закрыты, без "z"
)

func mascotGrid(m Mood) [][]byte {
	g := make([][]byte, len(mascotBase))
	for i, r := range mascotBase {
		g[i] = []byte(r)
	}
	eye := byte('T')
	switch m {
	case Upset:
		eye = 'R'
	case Asleep:
		eye = 'q'
		g[0][22], g[1][23] = 'Z', 'Z'
	}
	for i, on := range mascotEyes[m] {
		if on {
			for _, x := range []int{7, 8, 13, 14} {
				g[5+i][x] = eye
			}
		}
	}
	bar := map[Mood]byte{Happy: 'G', Upset: 'R'}[m]
	for x := 4; bar != 0 && x < 18; x++ {
		g[10][x] = bar
	}
	return g
}

// Mascot: строки картинки для терминала. Без хотя бы 256 цветов рисовать нечем - пусто.
func Mascot(m Mood, d Depth) []string {
	if d == Basic {
		return nil
	}
	g := mascotGrid(m)
	var lines []string
	for y := 0; y+1 < len(g); y += 2 {
		var b strings.Builder
		for x := 0; x+1 < len(g[y]); x += 2 {
			b.WriteString(quadCell([4]byte{g[y][x], g[y][x+1], g[y+1][x], g[y+1][x+1]}, d))
		}
		b.WriteString("\x1b[0m")
		lines = append(lines, b.String())
	}
	return lines
}

// quadCell: клетка из четырёх пикселей - самая частая краска символом, вторая фоном.
func quadCell(px [4]byte, d Depth) string {
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
		return "\x1b[0m "
	}
	bits := 0
	for i, c := range px {
		if c == fg {
			bits |= 8 >> i
		}
	}
	glyph := string([]rune(quads)[bits])
	if bg == 0 {
		return "\x1b[0m\x1b[" + mascotPal[fg].FG(d, "") + "m" + glyph
	}
	return "\x1b[" + mascotPal[fg].FG(d, "") + ";" + mascotPal[bg].BG(d) + "m" + glyph
}
