package brand

import "strings"

// Маскот - импульсный контейнер: куб в три четверти, на крыше и боку рёбра, на передней грани экран
// с глазами и полоса готовности. Рисунков два, под клетку терминала около 1:2.4 (JetBrains Mono с
// интервалом 1.1; Menlo - 1:2), чтобы передняя грань была квадратной: мелкий - четвертинками клетки,
// 14 колонок на 6 строк; крупный - полублоками, 26 на 11, с экраном 16x10, на котором видно настроение.
var mascotPal = map[byte]RGB{
	'U': {168, 176, 196}, // крыша
	'u': {132, 140, 160}, // её рёбра
	'h': {128, 136, 154}, // кромка крыши
	'N': {104, 112, 131}, // передняя грань: светлее фона любого тёмного терминала
	'n': {76, 82, 98},    // бок
	'm': {60, 65, 79},    // его рёбра
	'c': {78, 85, 101},   // уголки контейнера
	'E': {12, 22, 19},    // экран
	'k': {44, 48, 58},    // пустая полоса
	'T': {52, 196, 184},  // глаза: ярче фирменного бирюзового, на тёмном экране светятся
	'W': {220, 255, 250}, // блик в глазу
	'q': {24, 100, 95},   // глаза во сне
	'G': Dark[Green],     // готово
	'Y': Dark[Amber],     // тревога
	'R': Dark[Red],       // ошибка
	'Z': {140, 170, 255}, // z во сне
}

// mascotSmall: четвертинки - в клетке две краски, поэтому глаза e и полоса k не делят клетку с корпусом.
var mascotSmall = []string{
	"............................",
	"............................",
	"....UUUUUUUUUUUUUUUUUUUUUU..",
	"....uuuuuuuuuuuuuuuuuuuuuu..",
	"..NNNNNNNNNNNNNNNNNNNNNNnm..",
	"..NNEEEEEEEEEEEEEEEEEENNnm..",
	"..NNEEEEeeEEEEEEeeEEEENNnm..",
	"..NNEEEEeeEEEEEEeeEEEENNnm..",
	"..NNEEEEeeEEEEEEeeEEEENNnm..",
	"..NNEEEEEEEEEEEEEEEEEENNnm..",
	"..NNkkkkkkkkkkkkkkkkkkNN....",
	"..NNNNNNNNNNNNNNNNNNNNNN....",
}

// mascotLarge: полублоки - пиксель почти квадратный; крыша и бок со скосом, уголки контейнера,
// экран E 16x10 со строки 8 и колонки 4, полоса k.
var mascotLarge = []string{
	"..........................",
	"..........................",
	"......UUUUUUUUUUUUUUUUUUUU",
	".....uuuuuuuuuuuuuuuuuuuum",
	"....UUUUUUUUUUUUUUUUUUUUnm",
	"...uuuuuuuuuuuuuuuuuuuumnm",
	"..hhhhhhhhhhhhhhhhhhhhnmnm",
	"..ccNNNNNNNNNNNNNNNNccnmnm",
	"..ccEEEEEEEEEEEEEEEEccnmnm",
	"..NNEEEEEEEEEEEEEEEENNnmnm",
	"..NNEEEEEEEEEEEEEEEENNnmnm",
	"..NNEEEEEEEEEEEEEEEENNnmnm",
	"..NNEEEEEEEEEEEEEEEENNnmnm",
	"..NNEEEEEEEEEEEEEEEENNnmnm",
	"..NNEEEEEEEEEEEEEEEENNnmnm",
	"..NNEEEEEEEEEEEEEEEENNnmnm",
	"..NNEEEEEEEEEEEEEEEENNnmnm",
	"..NNEEEEEEEEEEEEEEEENNnmnm",
	"..NNNNNNNNNNNNNNNNNNNNnmn.",
	"..NNkkkkkkkkkkkkkkkkNNnm..",
	"..ccNNNNNNNNNNNNNNNNccn...",
	"..ccNNNNNNNNNNNNNNNNcc....",
}

// mascotFaces: левый глаз на экране крупного, 4x5: # горит, W блик; правый - такой же, у тревоги отражён.
var mascotFaces = map[Mood][]string{
	Calm:    {".##.", "#W##", "####", "####", ".##."},
	Happy:   {"....", ".##.", "#..#", "....", "...."},
	Worried: {"..##", ".###", "####", "####", ".##."},
	Upset:   {"#..#", ".##.", ".##.", "#..#", "...."},
	Asleep:  {"....", "....", "#..#", ".##.", "...."},
}

// blinkFace: кадр моргания - глаза закрыты, цвет и полоса остаются от настроения.
var blinkFace = []string{"....", "....", "....", "####", "...."}

// mascotInk и mascotBar: цвет глаз и полосы по настроению; полосы нет - пустая.
var (
	mascotInk = map[Mood]byte{Calm: 'T', Happy: 'T', Worried: 'Y', Upset: 'R', Asleep: 'q'}
	mascotBar = map[Mood]byte{Happy: 'G', Worried: 'Y', Upset: 'R'}
)

// Четвертинки по битам: левый верх, правый верх, левый низ, правый низ.
const quads = " \u2597\u2596\u2584\u259d\u2590\u259e\u259f\u2598\u259a\u258c\u2599\u2580\u259c\u259b\u2588"

const (
	halfUp   = "\u2580"
	halfDown = "\u2584"
)

// MascotWidth и MascotLargeWidth: ширина мелкой и крупной картинки в колонках терминала.
const (
	MascotWidth      = 14
	MascotLargeWidth = 26
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

// smallGrid: у мелкого глаза - три ряда сегментов: рад - верхние, ошибка - нижние, сон - один нижний.
func smallGrid(m Mood) [][]byte {
	m, shut := pose(m)
	g := copyRows(mascotSmall)
	from, to := 0, 3
	switch {
	case m == Happy:
		to = 2
	case m == Upset:
		from = 1
	case m == Asleep, shut:
		from = 2
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
		g[0][26], g[1][27] = 'Z', 'Z'
	}
	return g
}

// largeGrid: у крупного на экране рисуется лицо настроения.
func largeGrid(m Mood) [][]byte {
	m, shut := pose(m)
	g := copyRows(mascotLarge)
	left := mascotFaces[m]
	if shut {
		left = blinkFace
	}
	right := left
	if m == Worried {
		right = make([]string, len(left))
		for i, r := range left {
			b := []byte(r)
			for a, z := 0, len(b)-1; a < z; a, z = a+1, z-1 {
				b[a], b[z] = b[z], b[a]
			}
			right[i] = string(b)
		}
	}
	ink := mascotInk[m]
	for _, eye := range []struct {
		rows []string
		x    int
	}{{left, 7}, {right, 13}} {
		for dy, r := range eye.rows {
			for dx, c := range r {
				switch c {
				case '#':
					g[10+dy][eye.x+dx] = ink
				case 'W':
					g[10+dy][eye.x+dx] = 'W'
				}
			}
		}
	}
	paintBar(g, m)
	if m == Asleep {
		g[0][24], g[1][25] = 'Z', 'Z'
	}
	return g
}

// Mascot: мелкая картинка для терминала. Без хотя бы 256 цветов рисовать нечем - пусто.
func Mascot(m Mood, d Depth) []string {
	if d == Basic {
		return nil
	}
	g := smallGrid(m)
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

// MascotLarge: крупная картинка - полублоками, два пикселя на клетку по высоте. Без 256 цветов - пусто.
func MascotLarge(m Mood, d Depth) []string {
	if d == Basic {
		return nil
	}
	g := largeGrid(m)
	var lines []string
	for y := 0; y+1 < len(g); y += 2 {
		var b strings.Builder
		for x := range g[y] {
			b.WriteString(halfCell(g[y][x], g[y+1][x], d))
		}
		b.WriteString("\x1b[0m")
		lines = append(lines, b.String())
	}
	return lines
}

// halfCell: верхний пиксель символом, нижний фоном; одинаковые - пробел цветом фона, без символа.
func halfCell(top, bot byte, d Depth) string {
	switch {
	case top == '.' && bot == '.':
		return "\x1b[0m "
	case top == bot:
		return "\x1b[0m\x1b[" + mascotPal[top].BG(d) + "m "
	case bot == '.':
		return "\x1b[0m\x1b[" + mascotPal[top].FG(d, "") + "m" + halfUp
	case top == '.':
		return "\x1b[0m\x1b[" + mascotPal[bot].FG(d, "") + "m" + halfDown
	}
	return "\x1b[" + mascotPal[top].FG(d, "") + ";" + mascotPal[bot].BG(d) + "m" + halfUp
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
