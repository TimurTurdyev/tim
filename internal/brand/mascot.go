package brand

import "strings"

// Маскот - импульсный контейнер анфас: крыша в рёбрах, ручки по бокам, ножки, на передней грани экран
// с глазами и полоса готовности. Значок рисуется четвертинками клетки, 14 колонок на 4 строки: в клетке
// терминала около 1:2.4 (JetBrains Mono с интервалом 1.1; Menlo - 1:2) корпус выходит шире, чем выше.
var mascotPal = map[byte]RGB{
	'U': {168, 176, 196}, // крыша
	'u': {132, 140, 160}, // её рёбра
	'N': {104, 112, 131}, // передняя грань: светлее фона любого тёмного терминала
	'n': {76, 82, 98},    // ручки
	'E': {12, 22, 19},    // экран
	'k': {44, 48, 58},    // пустая полоса
	'T': {52, 196, 184},  // глаза: ярче фирменного бирюзового, на тёмном экране светятся
	'q': {24, 100, 95},   // глаза во сне
	'G': Dark[Green],     // полоса: готово
	'y': Dark[Amber],     // полоса: тревога
	'r': Dark[Red],       // полоса: ошибка
	'Y': Dark[Amber],     // глаза в тревоге: на тёмном экране яркие в любой теме
	'R': Dark[Red],       // глаза при ошибке
	'Z': {140, 170, 255}, // z во сне
}

// mascotLight: корпус для светлого фона - на белом светлая крыша пропадала.
var mascotLight = map[byte]RGB{
	'U': {118, 126, 148}, 'u': {94, 102, 122}, 'N': {82, 90, 108}, 'n': {58, 64, 78},
	'G': Light[Green], 'y': Light[Amber], 'r': Light[Red],
}

// mascotCompact: значок для лок-апа - 14 колонок на 4 строки; глаза 2x2. В клетке 2x2 пикселя не больше
// двух красок, считая пустоту: третью терминал не нарисует. Между глазами и полосой ряд экрана, иначе
// у тревоги и ошибки они сливаются одним цветом.
var mascotCompact = []string{
	"....uUuUuUuUuUuUuUuUuUuU....",
	"..UUUUUUUUUUUUUUUUUUUUUUUU..",
	"..NNEEEEEEEEEEEEEEEEEEEENN..",
	"nnNNEEEEeeEEEEEEEEeeEEEENNnn",
	".nNNEEEEeeEEEEEEEEeeEEEENNn.",
	"..NNEEEEEEEEEEEEEEEEEEEENN..",
	"..NNkkkkkkkkkkkkkkkkkkkkNN..",
	"..NNNN................NNNN..",
}

// mascotInk и mascotBar: цвет глаз и полосы по настроению; полосы нет - пустая.
var (
	mascotInk = map[Mood]byte{Calm: 'T', Happy: 'T', Worried: 'Y', Upset: 'R', Asleep: 'q'}
	mascotBar = map[Mood]byte{Happy: 'G', Worried: 'y', Upset: 'r'}
)

// Четвертинки по битам: левый верх, правый верх, левый низ, правый низ.
const quads = " \u2597\u2596\u2584\u259d\u2590\u259e\u259f\u2598\u259a\u258c\u2599\u2580\u259c\u259b\u2588"

// MascotCompactWidth и MascotCompactHeight: размер значка в клетках терминала.
const (
	MascotCompactWidth  = 14
	MascotCompactHeight = 4
)

// Mood: выражение лица.
type Mood int

const (
	Calm Mood = iota
	Happy
	Upset
	Asleep
	Worried // тревога: что-то подходит к краю

	// Blink: флаг кадра моргания поверх настроения, brand.Calm|brand.Blink; закрытые глаза не моргают.
	Blink Mood = 1 << 4
)

// Look: каким нарисовать маскота.
type Look struct {
	Mood  Mood // с флагом Blink на кадре моргания
	Light bool // корпус для светлого фона
}

// pose: настроение без флага и закрыты ли на этом кадре открытые глаза.
func pose(m Mood) (Mood, bool) {
	base := m &^ Blink
	return base, m&Blink != 0 && (base == Calm || base == Worried)
}

// copyRows: рисунок для правки.
func copyRows(rows []string) [][]byte {
	g := make([][]byte, len(rows))
	for i, r := range rows {
		g[i] = []byte(r)
	}
	return g
}

// paintBar: полоса готовности цветом настроения.
func paintBar(g [][]byte, m Mood) {
	bar, ok := mascotBar[m]
	if !ok {
		return
	}
	for _, row := range g {
		for x, c := range row {
			if c == 'k' {
				row[x] = bar
			}
		}
	}
}

// compactGrid: у значка два ряда: рад - верхний, ошибка и сон - нижний, цвет по настроению.
func compactGrid(l Look) [][]byte {
	return eyeRows(mascotCompact, l.Mood, 2)
}

// eyeRows: глаза из rows рядов сегментов "e": какие горят, решает настроение.
func eyeRows(src []string, m Mood, rows int) [][]byte {
	m, shut := pose(m)
	g := copyRows(src)
	from, to := 0, rows
	switch {
	case m == Happy:
		to = rows - 1
	case m == Upset:
		from = 1
	case shut:
		from = rows
	case m == Asleep:
		from = rows - 1
	}
	row := 0
	for _, r := range g {
		if !strings.Contains(string(r), "e") {
			continue
		}
		for x, c := range r {
			if c == 'e' {
				r[x] = 'E'
				if row >= from && row < to {
					r[x] = mascotInk[m]
				}
			}
		}
		row++
	}
	paintBar(g, m)
	if m == Asleep {
		g[0][len(g[0])-2], g[1][len(g[1])-1] = 'Z', 'Z'
	}
	return g
}

// palette: краски маскота под фон; экран, глаза и бегунок одинаковые в обеих темах.
func palette(light bool) map[byte]RGB {
	if !light {
		return mascotPal
	}
	p := make(map[byte]RGB, len(mascotPal))
	for k, v := range mascotPal {
		p[k] = v
	}
	for k, v := range mascotLight {
		p[k] = v
	}
	return p
}

// quadLines: сетка пикселей четвертинками, два пикселя на клетку по обеим осям.
func quadLines(g [][]byte, pal map[byte]RGB, d Depth) []string {
	var lines []string
	for y := 0; y+1 < len(g); y += 2 {
		var b strings.Builder
		for x := 0; x+1 < len(g[y]); x += 2 {
			b.WriteString(quadCell([4]byte{g[y][x], g[y][x+1], g[y+1][x], g[y+1][x+1]}, pal, d))
		}
		b.WriteString("\x1b[0m")
		lines = append(lines, b.String())
	}
	return lines
}

// MascotCompact: значок для лок-апа. Без хотя бы 256 цветов рисовать нечем - пусто.
func MascotCompact(l Look, d Depth) []string {
	if d == Basic {
		return nil
	}
	return quadLines(compactGrid(l), palette(l.Light), d)
}

// quadCell: клетка из четырёх пикселей - самая частая краска символом, вторая фоном.
func quadCell(px [4]byte, pal map[byte]RGB, d Depth) string {
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
		return "\x1b[0m\x1b[" + pal[fg].FG(d, "") + "m" + glyph
	}
	return "\x1b[" + pal[fg].FG(d, "") + ";" + pal[bg].BG(d) + "m" + glyph
}
