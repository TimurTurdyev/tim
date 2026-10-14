package brand

import "strings"

// Маскот - импульсный контейнер: куб в три четверти, сверху и сбоку рёбра, на передней грани
// панель-индикатор с глазами-сегментами и полоса готовности. Рисунок 24x12 пикселей: крупно - пиксель
// на клетку (24 колонки на 12 строк), мелко - четвертинками, 12 колонок на 6 строк. В четвертинке две
// краски: одна символом, другая фоном, поэтому глаза и полоса не делят клетку с корпусом.
var mascotPal = map[byte]RGB{
	'U': {168, 176, 196}, // верхняя грань
	'u': {132, 140, 160}, // её рёбра
	'N': {104, 112, 131}, // передняя грань: светлее фона любого тёмного терминала
	'n': {76, 82, 98},    // бок
	'm': {60, 65, 79},    // его рёбра
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
	"....UuUuUuUuUuUuUuUu....",
	"....UuUuUuUuUuUuUuUu....",
	"..NNNNNNNNNNNNNNNNnm....",
	"..NNEEEEEEEEEEEENNnm....",
	"..NNEEEEEEEEEEEENNnm....",
	"..NNEEEEEEEEEEEENNnm....",
	"..NNEEEEEEEEEEEENNnm....",
	"..NNEEEEEEEEEEEENNnm....",
	"..NNkkkkkkkkkkkkNN......",
	"..NNNNNNNNNNNNNNNN......",
}

// Глаза-сегменты: какие из трёх рядов панели горят. Ряд над ними делит клетку с корпусом, ряд под
// ними пустой - иначе глаза сливаются с полосой.
var mascotEyes = map[Mood][3]bool{
	Calm:   {true, true, true},
	Happy:  {true, true, false},
	Upset:  {false, true, true},
	Asleep: {false, false, true},
	Blink:  {false, false, true},
}

// Четвертинки по битам: левый верх, правый верх, левый низ, правый низ.
const quads = " \u2597\u2596\u2584\u259d\u2590\u259e\u259f\u2598\u259a\u258c\u2599\u2580\u259c\u259b\u2588"

// MascotWidth и MascotLargeWidth: ширина мелкой и крупной картинки в колонках терминала.
const (
	MascotWidth      = 12
	MascotLargeWidth = 24
)

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
			for _, x := range []int{6, 7, 12, 13} {
				g[6+i][x] = eye
			}
		}
	}
	bar := map[Mood]byte{Happy: 'G', Upset: 'R'}[m]
	for x := 4; bar != 0 && x < 16; x++ {
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

// MascotLarge: та же картинка крупно - пиксель на клетку, цветом фона. Символов нет, поэтому шрифт и
// межстрочный интервал терминала на неё не влияют. Без 256 цветов - пусто.
func MascotLarge(m Mood, d Depth) []string {
	if d == Basic {
		return nil
	}
	var lines []string
	for _, row := range mascotGrid(m) {
		var b strings.Builder
		prev := byte(0)
		for _, c := range row {
			if c != prev {
				if c == '.' {
					b.WriteString("\x1b[0m")
				} else {
					b.WriteString("\x1b[" + mascotPal[c].BG(d) + "m")
				}
				prev = c
			}
			b.WriteString(" ")
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
