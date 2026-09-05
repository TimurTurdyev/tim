package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/TimurTurdyev/tim/internal/ui"
)

// Живой экран железа: полосы с историей за минуту, самые занятые приложения, вывод словами.

// HWSample: один замер для живого экрана.
type HWSample struct {
	Title               string // паспорт: модель, ядра, память
	CPU, GPU, Mem       int    // проценты
	GPUValid            bool
	VRAM                int64
	MemUsed, MemTotal   int64
	SwapUsed, SwapTotal int64
	Load                []float64
	Disk                int // процент занятого
	Top                 []TopRow
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
	Procs  func() Screen   // tab
}

const histLen = 60 // минута истории при замере раз в секунду

type hw struct {
	opts   HWOpts
	last   HWSample
	have   bool
	paused bool
	busy   bool // замер идёт: второй не запускаем, иначе пауза плодит циклы
	cpuH   []float64
	gpuH   []float64
	memH   []float64
}

type hwSampleMsg HWSample

// NewHW создаёт живой экран железа.
func NewHW(o HWOpts) Screen { return &hw{opts: o} }

func (h *hw) Init() tea.Cmd { return h.fetch() }

func (h *hw) fetch() tea.Cmd {
	if h.busy {
		return nil
	}
	h.busy = true
	return func() tea.Msg { return hwSampleMsg(h.opts.Sample()) }
}

func (h *hw) Crumbs() []string { return []string{"Машина", "Железо"} }

func (h *hw) Status() string {
	switch {
	case !h.have:
		return "первый замер..."
	case h.paused:
		return h.last.Title + " " + symDot + " пауза"
	}
	return h.last.Title + " " + symDot + " обновляется"
}

func (h *hw) Command() string { return "tim hw --watch" }

func (h *hw) Keys() []Key {
	return []Key{{"tab", "процессы"}, {"space", "пауза"}, {"esc", "назад"}}
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
		case "tab", "p", "з":
			if h.opts.Procs != nil {
				return app.Push(h.opts.Procs())
			}
		case "space", " ":
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
	var out []string
	out = append(out, "")
	if !h.have {
		out = append(out, "  "+styleEmpty.Render("первый замер, секунду..."))
		return strings.Join(out, "\n")
	}
	s := h.last
	vram := ""
	if s.GPUValid {
		vram = "видеопамять " + ui.Bytes(s.VRAM)
	}
	mem := ui.Bytes(s.MemUsed) + " из " + ui.Bytes(s.MemTotal)
	if s.SwapTotal > 0 {
		mem += ", swap " + ui.Bytes(s.SwapUsed)
	}
	// Сначала место под цифры справа, остаток делят полоса и график; на узком экране график уходит.
	rest := width - 2 - 11 - 6 - max(ui.Width(vram), ui.Width(mem)) - 3 - 2
	barW := min(40, max(16, rest*11/20))
	sparkW := min(30, rest-barW-3)
	row := func(label string, pct int, hist []float64, tail string) string {
		line := "  " + styleDim.Render(ui.Fit(label, 11)) + Bar(float64(pct)/100, barW) + "  " + pctLevel(pct).Render(fmt.Sprintf("%3d%%", pct))
		if hist != nil && sparkW >= 8 {
			line += "   " + spark(hist, sparkW, pct)
		}
		if tail != "" {
			line += "   " + styleDim.Render(tail)
		}
		return line
	}
	out = append(out, row("процессор", s.CPU, h.cpuH, ""))
	if s.GPUValid {
		out = append(out, row("видеокарта", s.GPU, h.gpuH, vram))
	}
	out = append(out, row("память", s.Mem, h.memH, mem))
	if s.Disk > 0 {
		out = append(out, row("диск", s.Disk, nil, ""))
	}
	if len(s.Load) == 3 {
		out = append(out, "  "+styleDim.Render(fmt.Sprintf("%-11sнагрузка %.1f %.1f %.1f за 1, 5 и 15 минут", "", s.Load[0], s.Load[1], s.Load[2])))
	}
	out = append(out, "")
	if len(s.Top) > 0 {
		var rows []string
		for _, r := range s.Top {
			rows = append(rows, ui.Fit(ui.Clip(r.Name, 30), 32)+pctLevel(int(r.CPU/2)).Render(fmt.Sprintf("%5.0f%%", r.CPU))+"  "+styleDim.Render(ui.Fit(ui.Bytes(r.RSS), 9)))
		}
		for _, p := range Pane("больше всех", "CPU "+symDot+" память", rows, width-4) {
			out = append(out, "  "+p)
		}
	}
	if w := h.advice(); w != "" {
		out = append(out, "", "  "+styleAmber.Render(symWarn+" "+w)+styleDim.Render("  "+symArrow+"  tab к процессам"))
	}
	for len(out) < height {
		out = append(out, "")
	}
	return strings.Join(out[:min(len(out), height)], "\n")
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
		if r.RSS > topRSS {
			topRSS, byMem = r.RSS, r.Name
		}
	}
	switch {
	case s.SwapTotal > 0 && s.SwapUsed*2 > s.SwapTotal:
		w := "swap " + ui.Bytes(s.SwapUsed) + ": памяти не хватает"
		if byMem != "" {
			w += "; больше всех держит " + byMem
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
