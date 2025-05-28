package tui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/TimurTurdyev/tim/internal/fuzzy"
)

const maxRows = 10

var (
	styleTitle   = lipgloss.NewStyle().Foreground(colAccent).Bold(true)
	styleCount   = lipgloss.NewStyle().Foreground(colDim)
	styleHit     = lipgloss.NewStyle().Foreground(colOK).Bold(true)
	styleHint    = lipgloss.NewStyle().Foreground(colDim)
	styleRow     = lipgloss.NewStyle().Background(colRow).Bold(true)
	styleKey     = lipgloss.NewStyle().Foreground(colInfo)
	styleEmpty   = lipgloss.NewStyle().Foreground(colDim).Italic(true)
	stylePrompt  = lipgloss.NewStyle().Foreground(colAccent)
	styleChosen  = lipgloss.NewStyle().Foreground(colOK)
	styleEchoKey = lipgloss.NewStyle().Foreground(colDim)
	// Та же полоса слева, что у вопросов huh: все окна визарда выглядят одинаково.
	styleFrame = lipgloss.NewStyle().Border(lipgloss.ThickBorder(), false, false, false, true).
			BorderForeground(colAccent).PaddingLeft(1)
)

type picker struct {
	title   string
	input   textinput.Model
	items   []Item
	titles  []string
	matches []fuzzy.Match
	cursor  int
	chosen  int
	done    bool
	width   int // ширина колонки названий, чтобы подсказки стояли ровно
}

// Pick показывает список с поиском и возвращает индекс выбранного элемента.
func Pick(prompt string, items []Item) (int, error) {
	// Без этой проверки пикер в скрипте с перенаправленным stdin откроет /dev/tty и будет ждать.
	if !Available() {
		return -1, ErrNoTTY
	}
	tty, err := openTTY()
	if err != nil {
		return -1, ErrNoTTY
	}
	defer func() { _ = tty.Close() }()
	in := textinput.New()
	in.Prompt = symPointer + " "
	in.Placeholder = "начни печатать, чтобы отфильтровать"
	in.PromptStyle = stylePrompt
	in.PlaceholderStyle = styleHint
	in.Cursor.Style = lipgloss.NewStyle().Foreground(colAccent)
	in.Focus()
	p := &picker{title: prompt, input: in, items: items, chosen: -1}
	p.titles = make([]string, len(items))
	for i, it := range items {
		p.titles[i] = it.Title
		p.width = max(p.width, len([]rune(it.Title)))
	}
	p.width = min(p.width, 40)
	p.refilter()
	m, err := tea.NewProgram(p, tea.WithInput(tty), tea.WithOutput(tty)).Run()
	if err != nil {
		return -1, err
	}
	res := m.(*picker)
	if res.chosen < 0 {
		return -1, ErrCanceled
	}
	// После выбора список стирается; строка с ответом остаётся, чтобы в истории было видно, что выбрано.
	_, _ = fmt.Fprintln(tty, styleEchoKey.Render(prompt+":")+" "+styleChosen.Render(items[res.chosen].Title))
	return res.chosen, nil
}

func (p *picker) refilter() {
	p.matches = fuzzy.Filter(p.input.Value(), p.titles)
	if p.cursor >= len(p.matches) {
		p.cursor = max(0, len(p.matches)-1)
	}
}

func (p *picker) Init() tea.Cmd { return textinput.Blink }

func (p *picker) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if k, ok := msg.(tea.KeyMsg); ok {
		switch k.String() {
		case "esc", "ctrl+c":
			p.done = true
			return p, tea.Quit
		case "enter":
			if len(p.matches) > 0 {
				p.chosen = p.matches[p.cursor].Index
			}
			p.done = true
			return p, tea.Quit
		case "up", "ctrl+p", "ctrl+k":
			if p.cursor > 0 {
				p.cursor--
			}
			return p, nil
		case "down", "ctrl+n", "ctrl+j", "tab":
			if p.cursor < len(p.matches)-1 {
				p.cursor++
			}
			return p, nil
		}
	}
	var cmd tea.Cmd
	before := p.input.Value()
	p.input, cmd = p.input.Update(msg)
	if p.input.Value() != before {
		p.cursor = 0
		p.refilter()
	}
	return p, cmd
}

func (p *picker) View() string {
	if p.done {
		return ""
	}
	var b strings.Builder
	count := strconv.Itoa(len(p.matches)) + " из " + strconv.Itoa(len(p.items))
	b.WriteString(styleTitle.Render(p.title) + "  " + styleCount.Render(count) + "\n")
	b.WriteString(p.input.View() + "\n")
	if len(p.matches) == 0 {
		b.WriteString(styleEmpty.Render("ничего не подходит - сотри часть запроса") + "\n")
	}
	top := 0
	if p.cursor >= maxRows {
		top = p.cursor - maxRows + 1
	}
	for i := top; i < len(p.matches) && i < top+maxRows; i++ {
		m := p.matches[i]
		it := p.items[m.Index]
		title := clipTitle(it.Title, p.width)
		pad := strings.Repeat(" ", max(0, p.width-len([]rune(title))))
		if i == p.cursor {
			line := styleRow.Foreground(colAccent).Render(symPointer+" ") + highlight(title, m.Positions, true) + styleRow.Render(pad)
			if it.Hint != "" {
				line += styleRow.Render("  ") + styleRow.Foreground(colDim).Bold(false).Render(it.Hint)
			}
			b.WriteString(line + styleRow.Render(" ") + "\n")
			continue
		}
		line := "  " + highlight(title, m.Positions, false) + pad
		if it.Hint != "" {
			line += "  " + styleHint.Render(it.Hint)
		}
		b.WriteString(line + "\n")
	}
	if rest := len(p.matches) - (top + maxRows); rest > 0 {
		b.WriteString(styleHint.Render("  и ещё "+strconv.Itoa(rest)+" ниже") + "\n")
	}
	body := styleFrame.Render(strings.TrimRight(b.String(), "\n"))
	keys := []string{symUpDown + " выбор", "enter выбрать", "esc отмена"}
	for i, k := range keys {
		key, desc, _ := strings.Cut(k, " ")
		keys[i] = styleKey.Render(key) + " " + styleHint.Render(desc)
	}
	return body + "\n\n" + strings.Join(keys, styleHint.Render(" "+symDot+" "))
}

func clipTitle(s string, width int) string {
	r := []rune(s)
	if len(r) <= width {
		return s
	}
	return string(r[:width-1]) + "~"
}

// highlight подсвечивает буквы, совпавшие с запросом; на выделенной строке - на её фоне.
func highlight(s string, pos []int, selected bool) string {
	hit := make(map[int]bool, len(pos))
	for _, i := range pos {
		hit[i] = true
	}
	hitStyle := styleHit
	if selected {
		hitStyle = styleHit.Background(colRow)
	}
	var b strings.Builder
	for i, r := range []rune(s) {
		switch {
		case hit[i]:
			b.WriteString(hitStyle.Render(string(r)))
		case selected:
			b.WriteString(styleRow.Render(string(r)))
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
