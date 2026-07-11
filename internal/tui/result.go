package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/TimurTurdyev/tim/internal/level"
)

// result: вывод обычной команды внутри экрана. Сверху сама команда, чтобы запомнить.
type result struct {
	run    func() (string, error)
	title  string
	lines  []string
	err    error
	top    int
	done   bool
	copied func(string) // скопировать команду в буфер
	next   []CommandEntry
}

type resultMsg struct {
	out string
	err error
}

// ResultOpts: как показывать результат команды.
type ResultOpts struct {
	Title string
	Copy  func(text string)
	Next  []CommandEntry
}

func newResult(run func() (string, error)) *result {
	return &result{run: run, title: "команда"}
}

// NewResult: экран результата с заголовком и действиями "дальше".
func NewResult(title string, run func() (string, error), opts ResultOpts) Screen {
	r := newResult(run)
	r.title = title
	r.copied = opts.Copy
	r.next = opts.Next
	return r
}

func (r *result) Init() tea.Cmd {
	return func() tea.Msg {
		out, err := r.run()
		return resultMsg{out, err}
	}
}

func (r *result) Crumbs() []string { return []string{r.title} }

func (r *result) Status() string {
	if !r.done {
		return "выполняется..."
	}
	if r.err != nil {
		return "ошибка"
	}
	return "готово"
}

func (r *result) Keys() []Key {
	k := []Key{{symUpDown, "прокрутка"}, {"r", "повторить"}}
	if r.copied != nil {
		k = append(k, Key{"c", "скопировать команду"})
	}
	k = append(k, Key{"esc", "назад"})
	return k
}

func (r *result) Command() string { return "tim " + r.title }

func (r *result) Update(msg tea.Msg, app *App) tea.Cmd {
	switch msg := msg.(type) {
	case resultMsg:
		r.done = true
		r.err = msg.err
		r.lines = strings.Split(strings.TrimRight(msg.out, "\n"), "\n")
		if msg.err != nil {
			r.lines = append(r.lines, "", styleBad.Render(symCross+" "+msg.err.Error()))
		}
		return nil
	case tea.KeyPressMsg:
		switch msg.String() {
		case "up", "k":
			r.top = max(0, r.top-1)
		case "down", "j":
			r.top++
		case "pgup":
			r.top = max(0, r.top-20)
		case "pgdown":
			r.top += 20
		case "r", "к":
			r.done = false
			r.lines = nil
			return r.Init()
		case "c", "с":
			if r.copied != nil {
				r.copied("tim " + r.title)
				return app.Toast(level.OK, "скопировано: tim "+r.title)
			}
		case "1", "2", "3":
			i := int(msg.String()[0] - '1')
			if i < len(r.next) && app.opts.RunCommand != nil {
				return app.Do(app.opts.RunCommand(r.next[i].Line))
			}
		}
	}
	return nil
}

func (r *result) View(width, height int) string {
	var out []string
	out = append(out, "", "  "+stylePrompt.Render("$ ")+styleBold.Render("tim "+r.title), "")
	if !r.done {
		out = append(out, "  "+styleEmpty.Render("выполняю..."))
		return strings.Join(out, "\n")
	}
	body := height - len(out) - 1
	if len(r.next) > 0 {
		body -= 4
	}
	if r.top > max(0, len(r.lines)-body) {
		r.top = max(0, len(r.lines)-body)
	}
	for i := r.top; i < len(r.lines) && i < r.top+body; i++ {
		out = append(out, "  "+r.lines[i])
	}
	if len(r.next) > 0 {
		for len(out) < height-4 {
			out = append(out, "")
		}
		var parts []string
		for i, n := range r.next {
			if i >= 3 {
				break
			}
			parts = append(parts, styleKeyCap.Render(" "+string(rune('1'+i))+" ")+" "+styleText.Render("tim "+n.Line))
		}
		out = append(out, "  "+strings.Join(parts, "     "))
	}
	return strings.Join(out, "\n")
}
