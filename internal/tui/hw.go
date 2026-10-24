package tui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/TimurTurdyev/tim/internal/ui"
)

// Живой экран железа: полосы с историей за минуту, две панели самых занятых - по процессору и по
// памяти, вывод словами под ними.

// HWSample: один замер для живого экрана.
type HWSample struct {
	Title               string // паспорт: модель, ядра, память
	CPU, GPU, Mem       int    // проценты
	GPUValid            bool
	VRAM                int64
	MemUsed, MemTotal   int64
	SwapUsed, SwapTotal int64
	Load                []float64
	Disk                int   // процент занятого
	DiskFree            int64 // свободно, байт; 0 - неизвестно
	Cores               int
	Top                 []TopRow // по процессору
	TopMem              []TopRow // по памяти
}

// TopRow: строка списка самых занятых.
type TopRow struct {
	Name string
	CPU  float64
	RSS  int64
}

// HWOpts: источники живого экрана.
type HWOpts struct {
	Sample func() HWSample // длится около секунды и задаёт такт
}

// SwitchTab: вкладка просит открыть соседнюю - Железо ведёт к Процессам.
type SwitchTab int

const histLen = 60 // минута истории при замере раз в секунду

type hw struct {
	opts    HWOpts
	last    HWSample
	have    bool
	paused  bool
	busy    bool // замер идёт: второй не запускаем, иначе пауза плодит циклы
	started time.Time
	cpuH    []float64
	gpuH    []float64
	memH    []float64
}

type hwSampleMsg HWSample

// NewHW создаёт живой экран железа - вкладку Машины.
func NewHW(o HWOpts) Tab { return &hw{opts: o} }

func (h *hw) TabName() string { return "Железо" }

func (h *hw) OrderText() string {
	if h.paused {
		return "пауза"
	}
	return "раз в секунду"
}

func (h *hw) Init() tea.Cmd {
	h.started = time.Now()
	return h.fetch()
}

func (h *hw) fetch() tea.Cmd {
	if h.busy {
		return nil
	}
	h.busy = true
	return func() tea.Msg { return hwSampleMsg(h.opts.Sample()) }
}

func (h *hw) Crumbs() []string { return []string{"Машина"} }

func (h *hw) Status() string {
	switch {
	case !h.have:
		return spinFrames[0] + " первый замер"
	case h.paused:
		return h.last.Title + " " + symSep + " пауза"
	}
	return h.last.Title
}

func (h *hw) Command() string { return "tim hw --watch" }

func (h *hw) Keys() []Key {
	return []Key{{"enter", "процессы"}, {"z", "пауза"}}
}

func push(hist []float64, v int) []float64 {
	hist = append(hist, float64(v))
	if len(hist) > histLen {
		hist = hist[len(hist)-histLen:]
	}
	return hist
}

func (h *hw) Update(msg tea.Msg, app *App) tea.Cmd {
	switch msg := msg.(type) {
	case hwSampleMsg:
		h.last, h.have, h.busy = HWSample(msg), true, false
		h.cpuH = push(h.cpuH, h.last.CPU)
		h.gpuH = push(h.gpuH, h.last.GPU)
		h.memH = push(h.memH, h.last.Mem)
		if !h.paused {
			return h.fetch() // замер сам длится секунду - это и есть такт
		}
	case tea.KeyPressMsg:
		switch msg.String() {
		case "enter":
			return func() tea.Msg { return SwitchTab(1) }
		case "z":
			h.paused = !h.paused
			if !h.paused {
				return h.fetch()
			}
		}
	}
	return nil
}

func pctLevel(pct int) lipgloss.Style {
	switch {
	case pct >= 85:
		return styleBad
	case pct >= 60:
		return styleAmber
	}
	return styleGood
}

// spark: история процентов столбиками на постоянной шкале 0-100: спокойные 3% остаются у пола,
// а не растягиваются на всю высоту, как сделал бы график с подстройкой шкалы.
func spark(hist []float64, width int, pct int) string {
	bars := []rune{'\u2581', '\u2582', '\u2583', '\u2584', '\u2585', '\u2586', '\u2587', '\u2588'}
	if len(hist) > width {
		hist = hist[len(hist)-width:]
	}
	out := make([]rune, 0, width)
	for range width - len(hist) {
		out = append(out, ' ')
	}
	for _, v := range hist {
		i := int(v * float64(len(bars)) / 101)
		out = append(out, bars[max(0, min(len(bars)-1, i))])
	}
	return pctLevel(pct).Faint(true).Render(string(out))
}

func (h *hw) View(width, height int) string {
	if !h.have {
		return strings.Join(Notice("Первый замер", progressLine(-1, "процессор, память, видеокарта", h.started), "замер идёт раз в секунду, история набирается за минуту", width), "\n")
	}
	s := h.last
	sparkW := 14
	row := func(label string, pct int, hist []float64, tail string) string {
		line := "   " + styleAccent.Render(ui.Fit(label, 12)) + ui.Bar(pct, 20) + "  "
		if hist != nil {
			line += ui.Fit(spark(hist, sparkW, pct), sparkW+3)
		} else {
			line += strings.Repeat(" ", sparkW+3)
		}
		return line + styleDim.Render(tail)
	}
	out := []string{"", "   " + strings.Repeat(" ", 12+20+2+4) + styleHead.Render("ЗА МИНУТУ")}
	out = append(out, row("процессор", s.CPU, h.cpuH, ""))
	if s.GPUValid {
		out = append(out, row("видеокарта", s.GPU, h.gpuH, "видеопамять "+ui.Bytes(s.VRAM)))
	}
	mem := ui.Bytes(s.MemUsed) + " из " + ui.Bytes(s.MemTotal)
	if s.SwapTotal > 0 {
		mem += " " + symSep + " swap " + ui.Bytes(s.SwapUsed) + " из " + ui.Bytes(s.SwapTotal)
	}
	out = append(out, row("память", s.Mem, h.memH, mem))
	if s.Disk > 0 {
		free := ""
		if s.DiskFree > 0 {
			free = "свободно " + ui.Bytes(s.DiskFree)
		}
		out = append(out, row("диск", s.Disk, nil, free))
	}
	if len(s.Load) == 3 && s.Cores > 0 {
		pct := min(100, int(s.Load[0]*100/float64(s.Cores)))
		out = append(out, row("нагрузка", pct, nil, fmt.Sprintf("%.1f %.1f %.1f за 1, 5, 15 мин на %d %s",
			s.Load[0], s.Load[1], s.Load[2], s.Cores, ui.Plural(s.Cores, "ядре", "ядрах", "ядрах"))))
	}
	out = append(out, "")
	left := topPanel("ПО ПРОЦЕССОРУ", s.Top, true)
	right := topPanel("ПО ПАМЯТИ", s.TopMem, false)
	if width >= 120 {
		for i := range max(len(left), len(right)) {
			l, r := "", ""
			if i < len(left) {
				l = left[i]
			}
			if i < len(right) {
				r = right[i]
			}
			out = append(out, "   "+ui.Fit(l, 58)+"  "+r)
		}
	} else {
		for _, l := range left {
			out = append(out, "   "+l)
		}
	}
	if w := h.advice(); w != "" {
		out = append(out, "", "   "+styleAmber.Render(symWarn+" "+w))
	}
	return strings.Join(out[:min(len(out), height)], "\n")
}

// topPanel: самые занятые приложения со своей шапкой; по процессору спящие свёрнуты в строку.
func topPanel(title string, rows []TopRow, byCPU bool) []string {
	head := ui.Fit(title, 28) + fmt.Sprintf("%6s%9s", "CPU "+sortMark(true), "ПАМЯТЬ")
	if !byCPU {
		head = ui.Fit(title, 28) + fmt.Sprintf("%9s%7s", "ПАМЯТЬ "+sortMark(true), "CPU")
	}
	out := []string{styleHead.Render(head)}
	for _, r := range rows {
		if byCPU && r.CPU < 1 {
			out = append(out, styleDim.Render("остальные спят, меньше 1% CPU"))
			break
		}
		cpu := pctLevel(int(r.CPU)).Render(fmt.Sprintf("%5.0f%%", r.CPU))
		if byCPU {
			out = append(out, ui.Fit(r.Name, 28)+cpu+fmt.Sprintf("%9s", ui.Bytes(r.RSS)))
		} else {
			out = append(out, ui.Fit(r.Name, 28)+fmt.Sprintf("%9s", ui.Bytes(r.RSS))+"  "+cpu)
		}
	}
	return out
}

// advice: вывод словами - что не так и кто виноват.
func (h *hw) advice() string {
	s := h.last
	var byCPU, byMem string
	var topCPU float64
	var topRSS int64
	for _, r := range s.Top {
		if r.CPU > topCPU {
			topCPU, byCPU = r.CPU, r.Name
		}
	}
	for _, r := range append(s.TopMem, s.Top...) {
		if r.RSS > topRSS {
			topRSS, byMem = r.RSS, r.Name
		}
	}
	switch {
	case s.SwapTotal > 0 && s.SwapUsed*2 > s.SwapTotal:
		w := "swap " + ui.Bytes(s.SwapUsed) + ": памяти не хватает"
		if byMem != "" {
			w += ", больше всех держит " + byMem
		}
		return w
	case s.CPU >= 85:
		w := "процессор занят почти целиком"
		if byCPU != "" {
			w += ": " + byCPU + fmt.Sprintf(" %.0f%%", topCPU)
		}
		return w
	case s.Mem >= 90:
		w := "память почти кончилась"
		if byMem != "" {
			w += "; больше всех держит " + byMem + ", " + ui.Bytes(topRSS)
		}
		return w
	}
	return ""
}
