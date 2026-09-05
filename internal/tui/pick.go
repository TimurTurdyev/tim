package tui

import (
	"fmt"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/TimurTurdyev/tim/internal/fuzzy"
	"github.com/TimurTurdyev/tim/internal/ui"
)

const maxRows = 10

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
	setStyles(ui.Light())
	in := textinput.New()
	in.Prompt = symPointer + " "
	in.Placeholder = "начни печатать, чтобы отфильтровать"
	// Без ширины textinput обрезает подсказку до одной буквы: буфер под неё - ширина плюс один.
	in.SetWidth(48)
	styleInput(&in)
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

func (p *picker) Init() tea.Cmd { return tea.Batch(textinput.Blink, tea.RequestBackgroundColor) }

func (p *picker) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if bg, ok := msg.(tea.BackgroundColorMsg); ok {
		if useBackground(bg) {
			styleInput(&p.input)
		}
		return p, nil
	}
	if k, ok := msg.(tea.KeyPressMsg); ok {
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

func (p *picker) View() tea.View {
	if p.done {
		return tea.NewView("")
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
	return tea.NewView(body + "\n\n" + strings.Join(keys, styleHint.Render(" "+symDot+" ")))
}

func clipTitle(s string, width int) string {
	r := []rune(s)
	if len(r) <= width {
		return s
	}
	return string(r[:width-1]) + "~"
}

func styleInput(in *textinput.Model) {
	st := in.Styles()
	st.Focused.Prompt = stylePrompt
	st.Focused.Placeholder = styleHint
	st.Cursor.Color = colAccent
	in.SetStyles(st)
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
	// Один Render на участок подряд совпавших или не совпавших рун, а не на каждую руну.
	runes := []rune(s)
	var b strings.Builder
	for start := 0; start < len(runes); {
		end := start + 1
		for end < len(runes) && hit[end] == hit[start] {
			end++
		}
		part := string(runes[start:end])
		switch {
		case hit[start]:
			b.WriteString(hitStyle.Render(part))
		case selected:
			b.WriteString(styleRow.Render(part))
		default:
			b.WriteString(part)
		}
		start = end
	}
	return b.String()
}
