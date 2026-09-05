package brand

import "strings"

// Маскот - пиксельный мини-программист 12x12, рисуется полублоками: одна строка терминала - два пикселя.
var mascotPal = map[byte]RGB{
	'H': {58, 42, 34},    // волосы
	'S': {244, 196, 160}, // кожа
	'E': {30, 30, 40},    // глаза
	'W': {255, 255, 255}, // блик
	'M': {196, 92, 92},   // рот
	'R': {230, 60, 60},   // глаза при ошибке
	'T': Dark[Teal],      // худи
	'D': {25, 120, 115},  // тень худи
	'L': {150, 155, 165}, // ноутбук
	'G': Dark[Green],     // свечение экрана
	'K': {20, 24, 30},    // экран
	'Z': {140, 170, 255}, // z во сне
}

var mascotBase = []string{
	"..HHHHHH....",
	".HHHHHHHH...",
	".HSSSSSSH...",
	".S%e1SS%e2S...",
	".SSSSSSSS...",
	"..S%mS....",
	"...SSSS.....",
	".DTTTTTTD...",
	"DTTTTTTTTD..",
	"LLLLLLLLLL..",
	"LKGKKKKKKL..",
	"LLLLLLLLLL..",
}

const (
	halfUp   = "\u2580"
	halfDown = "\u2584"
)

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
	e1, e2, mouth := "EW", "EW", "SMMS"
	switch m {
	case Happy:
		e1, e2, mouth = "EE", "EE", "MSSM"
	case Upset:
		e1, e2 = "RR", "RR"
	case Asleep, Blink:
		e1, e2, mouth = "SS", "SS", "SSSS"
		if m == Blink {
			mouth = "SMMS"
		}
	}
	g := make([][]byte, len(mascotBase))
	for i, row := range mascotBase {
		r := strings.NewReplacer("%e1", e1, "%e2", e2, "%m", mouth).Replace(row)
		g[i] = []byte(r)
	}
	switch m {
	case Happy:
		// Глаза-дуги: улыбка до ушей.
		g[3][2], g[3][3], g[3][6], g[3][7] = 'E', 'S', 'S', 'E'
		g[4][2], g[4][3], g[4][6], g[4][7] = 'E', 'E', 'E', 'E'
	case Asleep:
		g[4][2], g[4][3], g[4][6], g[4][7] = 'E', 'E', 'E', 'E'
		g[0][10], g[1][11] = 'Z', 'Z'
	case Blink:
		g[4][2], g[4][3], g[4][6], g[4][7] = 'E', 'E', 'E', 'E'
	case Upset:
		g[0][10], g[1][10], g[3][10] = 'R', 'R', 'R'
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
		for x := range g[y] {
			top, bot := g[y][x], g[y+1][x]
			switch {
			case top == '.' && bot == '.':
				b.WriteString("\x1b[0m ")
			case bot == '.':
				b.WriteString("\x1b[0m\x1b[" + mascotPal[top].FG(d, "") + "m" + halfUp)
			case top == '.':
				b.WriteString("\x1b[0m\x1b[" + mascotPal[bot].FG(d, "") + "m" + halfDown)
			default:
				b.WriteString("\x1b[" + mascotPal[top].FG(d, "") + ";" + mascotPal[bot].BG(d) + "m" + halfUp)
			}
		}
		b.WriteString("\x1b[0m")
		lines = append(lines, b.String())
	}
	return lines
}
