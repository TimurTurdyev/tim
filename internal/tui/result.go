package tui

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/TimurTurdyev/tim/internal/level"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// result: вывод команды внутри экрана. Команда - в крошке и справа внизу, в теле только вывод; исход
// и время - в статусе; что логично сделать дальше - цифрами сразу под выводом.
type result struct {
	run     func() (string, error)
	title   string
	lines   []string
	out     string
	err     error
	top     int
	done    bool
	started time.Time
	took    time.Duration
	copied  func(string) // скопировать в буфер
	copyOut bool         // y копирует вывод, а не команду
	next    []CommandEntry
	height  int // сколько строк вывода видно - для счётчика прокрутки
}

type resultMsg struct {
	out string
	err error
}

// ResultOpts: как показывать результат команды. CopyOutput - у утилит без аргументов полезен
// сам вывод (uuid, пароль), его и копирует y.
type ResultOpts struct {
	Title      string
	Copy       func(text string)
	CopyOutput bool
	Next       []CommandEntry
}

func newResult(run func() (string, error)) *result {
	return &result{run: run, title: "команда"}
}

// NewResult: экран результата с заголовком и действиями "дальше".
func NewResult(title string, run func() (string, error), opts ResultOpts) Screen {
	r := newResult(run)
	r.title = title
	r.copied = opts.Copy
	r.copyOut = opts.CopyOutput
	r.next = opts.Next
	return r
}

func (r *result) Init() tea.Cmd {
	r.started = time.Now()
	return func() tea.Msg {
		out, err := r.run()
		return resultMsg{out, err}
	}
}

func (r *result) Crumbs() []string { return []string{r.title} }

func (r *result) Status() string {
	switch {
	case !r.done:
		return spinFrames[0] + " выполняю " + symSep + " " + ui.Short(time.Since(r.started))
	case r.err != nil:
		return symCross + " ошибка"
	case r.top > 0 || len(r.lines) > r.height && r.height > 0:
		end := min(len(r.lines), r.top+r.height)
		return "строки " + itoa(r.top+1) + "-" + itoa(end) + " из " + itoa(len(r.lines))
	}
	return symCheck + " готово " + symSep + " " + tookText(r.took)
}

func (r *result) Keys() []Key {
	if !r.done {
		return nil
	}
	var k []Key
	if len(r.next) > 0 {
		k = append(k, Key{"1-" + itoa(min(3, len(r.next))), "дальше"})
	}
	if r.copied != nil {
		k = append(k, Key{"y", "скопировать"})
	}
	return append(k, Key{"r", "повторить"})
}

func (r *result) Command() string { return "tim " + r.title }

func (r *result) Update(msg tea.Msg, app *App) tea.Cmd {
	switch msg := msg.(type) {
	case resultMsg:
		r.done, r.err, r.out = true, msg.err, msg.out
		r.took = time.Since(r.started)
		r.lines = strings.Split(strings.TrimRight(msg.out, "\n"), "\n")
		markOutcome(msg.err == nil)
		if msg.err != nil {
			r.lines = append(r.lines, "", styleBad.Render(symCross+" "+msg.err.Error()))
			return app.Say(level.Bad, r.title+": "+msg.err.Error())
		}
		return nil
	case tea.KeyPressMsg:
		page := max(1, r.height-1)
		switch msg.String() {
		case "up", "k":
			r.top = max(0, r.top-1)
		case "down", "j":
			r.top++
		case "pgup":
			r.top = max(0, r.top-page)
		case "pgdown", "space", " ":
			r.top += page
		case "home", "g":
			r.top = 0
		case "end", "G":
			r.top = len(r.lines)
		case "r":
			r.done, r.lines, r.top = false, nil, 0
			return r.Init()
		case "y":
			if r.copied == nil || !r.done {
				return nil
			}
			if r.copyOut {
				r.copied(strings.TrimSpace(ui.StripANSI(r.out)))
				return app.Say(level.OK, r.title+": вывод скопирован")
			}
			r.copied("tim " + r.title)
			return app.Say(level.OK, "скопировано: tim "+r.title)
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
	out := []string{""}
	if !r.done {
		return strings.Join(out, "\n")
	}
	var next []string
	if len(r.next) > 0 {
		parts := []string{styleDim.Render("дальше")}
		for i, n := range r.next {
			if i >= 3 {
				break
			}
			parts = append(parts, styleKeyCap.Render(" "+string(rune('1'+i))+" ")+" "+styleText.Render(n.Line))
		}
		next = []string{"", "   " + strings.Join(parts, "     ")}
	}
	body := height - len(out) - len(next)
	r.height = body
	if r.top > max(0, len(r.lines)-body) {
		r.top = max(0, len(r.lines)-body)
	}
	end := min(len(r.lines), r.top+body)
	for i := r.top; i < end; i++ {
		out = append(out, "   "+r.lines[i])
	}
	if end < len(r.lines) {
		out[len(out)-1] = styleDim.Render("   " + symDown + " ещё " + itoa(len(r.lines)-end) + " ниже")
	}
	return strings.Join(append(out, next...), "\n")
}
