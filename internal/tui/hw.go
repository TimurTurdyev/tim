package tui

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/TimurTurdyev/tim/internal/brand"
)

// HWSample: один замер для живого экрана.
type HWSample struct {
	CPU, GPU, Mem       int // проценты
	GPUValid            bool
	VRAM                int64
	MemUsed, MemTotal   int64
	SwapUsed, SwapTotal int64
	Load                []float64
	Top                 []TopRow
}

// TopRow: строка списка самых занятых.
type TopRow struct {
	Name string
	CPU  float64
	RSS  int64
}

const histLen = 60 // минута истории при замере раз в секунду

type hwModel struct {
	title   string
	sample  func() HWSample
	last    HWSample
	have    bool
	cpuH    []float64
	gpuH    []float64
	memH    []float64
	paused  bool
	blink   bool
	width   int
	started time.Time
}

type (
	sampleMsg HWSample
	blinkMsg  bool
)

func (m *hwModel) fetch() tea.Cmd {
	return func() tea.Msg { return sampleMsg(m.sample()) }
}

// nextBlink: моргает раз в несколько секунд, на долю секунды - иначе это не живой, а мигающий.
func nextBlink(closed bool) tea.Cmd {
	wait := 4*time.Second + time.Duration(time.Now().UnixNano()%2500)*time.Millisecond
	if closed {
		wait = 160 * time.Millisecond
	}
	return tea.Tick(wait, func(time.Time) tea.Msg { return blinkMsg(!closed) })
}

func (m *hwModel) Init() tea.Cmd { return tea.Batch(m.fetch(), nextBlink(false)) }

func push(h []float64, v int) []float64 {
	h = append(h, float64(v))
	if len(h) > histLen {
		h = h[len(h)-histLen:]
	}
	return h
}

func (m *hwModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "esc", "ctrl+c", "й":
			return m, tea.Quit
		case "p", "з", " ":
			m.paused = !m.paused
			if !m.paused {
				return m, m.fetch()
			}
		}
	case tea.WindowSizeMsg:
		m.width = msg.Width
	case blinkMsg:
		m.blink = bool(msg)
		return m, nextBlink(m.blink)
	case sampleMsg:
		m.last, m.have = HWSample(msg), true
		m.cpuH = push(m.cpuH, m.last.CPU)
		m.gpuH = push(m.gpuH, m.last.GPU)
		m.memH = push(m.memH, m.last.Mem)
		if !m.paused {
			return m, m.fetch() // замер сам длится секунду - это и есть такт
		}
	}
	return m, nil
}

// mood: маскот отражает машину - спит без нагрузки, сердится, когда тяжело.
func (m *hwModel) mood() brand.Mood {
	s := m.last
	switch {
	case !m.have:
		return brand.Calm
	case s.CPU >= 85 || s.GPU >= 90 || (s.SwapTotal > 0 && s.SwapUsed*5 > s.SwapTotal*4):
		return brand.Upset
	case s.CPU < 8 && (!s.GPUValid || s.GPU < 8):
		return brand.Asleep
	case m.blink:
		return brand.Blink
	}
	return brand.Calm
}

var (
	styleLabel = lipgloss.NewStyle().Foreground(colAccent).Width(12)
	styleDimHW = lipgloss.NewStyle().Foreground(colDim)
	styleWarn  = lipgloss.NewStyle().Foreground(lipgloss.Color(brand.Amber.Hex()))
	styleBadHW = lipgloss.NewStyle().Foreground(colBad)
	styleOKHW  = lipgloss.NewStyle().Foreground(colOK)
)

func levelStyle(pct int) lipgloss.Style {
	switch {
	case pct >= 85:
		return styleBadHW
	case pct >= 60:
		return styleWarn
	}
	return styleOKHW
}

func hwBar(pct, width int) string {
	pct = max(0, min(100, pct))
	full := pct * width / 100
	return levelStyle(pct).Render(strings.Repeat("\u2588", full)) + styleDimHW.Render(strings.Repeat("\u2591", width-full)) +
		" " + levelStyle(pct).Render(fmt.Sprintf("%3d%%", pct))
}

// spark: история процентов столбиками на постоянной шкале 0-100: спокойные 3% остаются у пола,
// а не растягиваются на всю высоту, как сделал бы график с подстройкой шкалы.
func spark(h []float64, width int, pct int) string {
	bars := []rune{'\u2581', '\u2582', '\u2583', '\u2584', '\u2585', '\u2586', '\u2587', '\u2588'}
	if len(h) > width {
		h = h[len(h)-width:]
	}
	out := make([]rune, 0, width)
	for range width - len(h) {
		out = append(out, ' ')
	}
	for _, v := range h {
		i := int(v * float64(len(bars)) / 101)
		out = append(out, bars[max(0, min(len(bars)-1, i))])
	}
	return levelStyle(pct).Faint(true).Render(string(out))
}

func gib(b int64) string {
	switch {
	case b >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(b)/(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%.0f MB", float64(b)/(1<<20))
	}
	return fmt.Sprintf("%d KB", b>>10)
}

func (m *hwModel) View() string {
	d := brand.DetectDepth(os.Getenv("COLORTERM"), os.Getenv("TERM"))
	mascot := brand.Mascot(m.mood(), d)
	head := []string{
		"",
		styleTitle.Render("tim hw") + "  " + styleDimHW.Render(m.title),
		styleDimHW.Render(time.Now().Format("15:04:05")),
	}
	s := m.last
	if len(s.Load) == 3 {
		head = append(head, styleDimHW.Render(fmt.Sprintf("load %.1f %.1f %.1f", s.Load[0], s.Load[1], s.Load[2])))
	}
	if m.paused {
		head = append(head, styleWarn.Render("пауза"))
	}
	var b strings.Builder
	for i := 0; i < max(len(mascot), len(head)); i++ {
		left := strings.Repeat(" ", brand.MascotWidth)
		if i < len(mascot) {
			left = mascot[i]
		}
		right := ""
		if i < len(head) {
			right = head[i]
		}
		b.WriteString("  " + left + "   " + right + "\n")
	}
	b.WriteString("\n")
	if !m.have {
		b.WriteString("  " + styleDimHW.Render("первый замер...") + "\n")
		return m.frame(b.String())
	}
	barW, sparkW := 22, 30
	if m.width > 0 && m.width < 90 {
		sparkW = max(10, m.width-62)
	}
	// Пустая строка между показателями: иначе графики истории трёх рядов слипаются в один блок.
	b.WriteString("  " + styleLabel.Render("процессор") + hwBar(s.CPU, barW) + "  " + spark(m.cpuH, sparkW, s.CPU) + "\n\n")
	if s.GPUValid {
		b.WriteString("  " + styleLabel.Render("видеокарта") + hwBar(s.GPU, barW) + "  " + spark(m.gpuH, sparkW, s.GPU) +
			"  " + styleDimHW.Render("видеопамять "+gib(s.VRAM)) + "\n\n")
	}
	b.WriteString("  " + styleLabel.Render("память") + hwBar(s.Mem, barW) + "  " + spark(m.memH, sparkW, s.Mem) +
		"  " + styleDimHW.Render(gib(s.MemUsed)+" из "+gib(s.MemTotal)) + "\n\n")
	if s.SwapTotal > 0 {
		sw := gib(s.SwapUsed) + " из " + gib(s.SwapTotal)
		if s.SwapUsed*2 > s.SwapTotal {
			sw = styleWarn.Render(sw + ", памяти не хватает")
		}
		b.WriteString("  " + styleLabel.Render("swap") + sw + "\n")
	}
	if len(s.Top) > 0 {
		b.WriteString("\n  " + styleTitle.Render("больше всех CPU") + "\n")
		for _, r := range s.Top {
			name := []rune(r.Name)
			if len(name) > 30 {
				name = append(name[:29], '~')
			}
			b.WriteString("    " + lipgloss.NewStyle().Width(32).Render(string(name)) +
				levelStyle(int(r.CPU/2)).Render(fmt.Sprintf("%6.1f%%", r.CPU)) + "  " + styleDimHW.Render(gib(r.RSS)) + "\n")
		}
	}
	return m.frame(b.String())
}

func (m *hwModel) frame(body string) string {
	keys := []string{"p пауза", "q выход"}
	for i, k := range keys {
		key, desc, _ := strings.Cut(k, " ")
		keys[i] = styleKey.Render(key) + " " + styleHint.Render(desc)
	}
	up := styleDimHW.Render("  смотрю " + strconv.Itoa(int(time.Since(m.started).Seconds())) + " с  " + symDot + "  ")
	return body + "\n" + up + strings.Join(keys, styleHint.Render(" "+symDot+" "))
}

// RunHW: живой экран загрузки до q или Esc. sample вызывается в фоне и может занимать секунду.
func RunHW(title string, sample func() HWSample) error {
	if !Available() {
		return ErrNoTTY
	}
	m := &hwModel{title: title, sample: sample, started: time.Now()}
	_, err := tea.NewProgram(m, tea.WithAltScreen()).Run()
	return err
}
