package tui

import (
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/TimurTurdyev/tim/internal/level"
	"github.com/TimurTurdyev/tim/internal/ui"
	"github.com/TimurTurdyev/tim/internal/view"
	"github.com/TimurTurdyev/tim/report"
)

// result: вывод команды внутри экрана. Команда - в крошке и справа внизу, в теле только вывод; исход
// и время - в статусе; что логично сделать дальше - цифрами сразу под выводом. Отчёт команды экран
// раскладывает сам под свою ширину, его таблицу сортирует p.
type result struct {
	run     func() (string, *report.Report, error)
	rep     *report.Report
	sortBy  int
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
	rep *report.Report
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
	return &result{run: func() (string, *report.Report, error) {
		out, err := run()
		return out, nil, err
	}, title: "команда"}
}

// NewResult: экран результата с заголовком и действиями "дальше".
func NewResult(title string, run func() (string, error), opts ResultOpts) Screen {
	r := newResult(run)
	r.setOpts(title, opts)
	return r
}

// NewReportResult: то же, но команда может отдать отчёт - тогда экран рисует его, а не текст.
func NewReportResult(title string, run func() (string, *report.Report, error), opts ResultOpts) Screen {
	r := &result{run: run}
	r.setOpts(title, opts)
	return r
}

func (r *result) setOpts(title string, opts ResultOpts) {
	r.title = title
	r.copied = opts.Copy
	r.copyOut = opts.CopyOutput
	r.next = opts.Next
}

func (r *result) Init() tea.Cmd {
	r.started = time.Now()
	return func() tea.Msg {
		out, rep, err := r.run()
		return resultMsg{out, rep, err}
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
	if len(r.columns()) > 0 {
		k = append(k, Key{"p", "порядок"})
	}
	return append(k, Key{"r", "повторить"})
}

func (r *result) Command() string { return "tim " + r.title }

func (r *result) Update(msg tea.Msg, app *App) tea.Cmd {
	switch msg := msg.(type) {
	case resultMsg:
		r.done, r.err, r.out, r.rep = true, msg.err, msg.out, msg.rep
		r.took = time.Since(r.started)
		r.lines = strings.Split(strings.TrimRight(msg.out, "\n"), "\n")
		if msg.rep != nil {
			r.lines = view.Lines(*msg.rep, view.Layout{})
			r.next = mergeNext(stepsOf(*msg.rep), r.next)
		}
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
			r.done, r.lines, r.top, r.rep = false, nil, 0, nil
			return r.Init()
		case "p":
			return r.sortNext(app)
		case "y":
			if r.copied == nil || !r.done {
				return nil
			}
			if r.copyOut {
				r.copied(strings.TrimSpace(ui.StripANSI(r.text())))
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
			shown := n.Line
			if n.Name != "" {
				shown = n.Name
			}
			parts = append(parts, styleKeyCap.Render(" "+string(rune('1'+i))+" ")+" "+styleText.Render(shown))
		}
		next = []string{"", "   " + strings.Join(parts, "     ")}
	}
	if r.rep != nil {
		r.lines = view.Lines(*r.rep, view.Layout{Width: width - 3, SortBy: r.sortBy, Runnable: true})
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

// text: что копирует y - отчёт без цветов или вывод команды.
func (r *result) text() string {
	if r.rep == nil {
		return r.out
	}
	return strings.Join(view.Lines(*r.rep, view.Layout{}), "\n")
}

// columns: колонки первой таблицы отчёта - по ним сортирует p.
func (r *result) columns() []report.Column {
	if r.rep == nil {
		return nil
	}
	for _, b := range r.rep.Blocks {
		if t, ok := b.(report.Table); ok {
			return t.Columns
		}
	}
	return nil
}

// sortNext: p по кругу - колонки слева направо, потом порядок самой команды.
func (r *result) sortNext(app *App) tea.Cmd {
	cols := r.columns()
	if len(cols) == 0 {
		return nil
	}
	r.sortBy = (r.sortBy + 1) % (len(cols) + 1)
	r.top = 0
	if r.sortBy == 0 {
		return app.Say(level.Info, "порядок как у команды")
	}
	return app.Say(level.Info, "по колонке "+strings.ToLower(cols[r.sortBy-1].Title))
}

// stepsOf: шаги "дальше" из отчёта - только команды tim, остальное экран не запускает. Одинаковые
// подписи (две карточки - два "остановить") заменяются самой командой.
func stepsOf(rep report.Report) []CommandEntry {
	var out []CommandEntry
	names := map[string]int{}
	for _, b := range rep.Blocks {
		next, ok := b.(report.Next)
		if !ok {
			continue
		}
		for _, s := range next {
			if line, ok := strings.CutPrefix(s.Command, "tim "); ok {
				out = append(out, CommandEntry{Line: line, Name: s.Text})
				names[s.Text]++
			}
		}
	}
	for i := range out {
		if names[out[i].Name] > 1 {
			out[i].Name = ""
		}
	}
	return out
}

func mergeNext(first, rest []CommandEntry) []CommandEntry {
	out := first
	for _, e := range rest {
		if !slices.ContainsFunc(out, func(o CommandEntry) bool { return o.Line == e.Line }) {
			out = append(out, e)
		}
	}
	return out
}
