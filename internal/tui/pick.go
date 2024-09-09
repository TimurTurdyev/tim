package tui

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/TimurTurdyev/tim/internal/fuzzy"
)

const maxRows = 12

var (
	styleHit    = lipgloss.NewStyle().Foreground(lipgloss.Color("2")).Bold(true)
	styleCursor = lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Bold(true)
	styleHint   = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	styleCount  = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
)

type picker struct {
	input   textinput.Model
	items   []Item
	titles  []string
	matches []fuzzy.Match
	cursor  int
	chosen  int
	done    bool
}

// Pick показывает список с fuzzy-поиском и возвращает индекс выбранного элемента.
func Pick(prompt string, items []Item) (int, error) {
	tty, err := openTTY()
	if err != nil {
		return -1, ErrNoTTY
	}
	defer func() { _ = tty.Close() }()
	in := textinput.New()
	in.Prompt = prompt + " > "
	in.Focus()
	p := &picker{input: in, items: items, chosen: -1}
	p.titles = make([]string, len(items))
	for i, it := range items {
		p.titles[i] = it.Title
	}
	p.refilter()
	m, err := tea.NewProgram(p, tea.WithInput(tty), tea.WithOutput(tty)).Run()
	if err != nil {
		return -1, err
	}
	res := m.(*picker)
	if res.chosen < 0 {
		return -1, ErrCanceled
	}
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
		case "up", "ctrl+p":
			if p.cursor > 0 {
				p.cursor--
			}
			return p, nil
		case "down", "ctrl+n":
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
	b.WriteString(p.input.View())
	b.WriteString("  " + styleCount.Render(strconv.Itoa(len(p.matches))+"/"+strconv.Itoa(len(p.items))) + "\n")
	top := 0
	if p.cursor >= maxRows {
		top = p.cursor - maxRows + 1
	}
	for i := top; i < len(p.matches) && i < top+maxRows; i++ {
		m := p.matches[i]
		mark := "  "
		if i == p.cursor {
			mark = styleCursor.Render("> ")
		}
		line := mark + highlight(p.items[m.Index].Title, m.Positions)
		if h := p.items[m.Index].Hint; h != "" {
			line += "  " + styleHint.Render(h)
		}
		b.WriteString(line + "\n")
	}
	return b.String()
}

func highlight(s string, pos []int) string {
	if len(pos) == 0 {
		return s
	}
	hit := make(map[int]bool, len(pos))
	for _, i := range pos {
		hit[i] = true
	}
	var b strings.Builder
	for i, r := range []rune(s) {
		if hit[i] {
			b.WriteString(styleHit.Render(string(r)))
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}
