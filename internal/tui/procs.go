package tui

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/TimurTurdyev/tim/internal/fuzzy"
	"github.com/TimurTurdyev/tim/internal/level"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// Процессы, сложенные по приложениям: по CPU - группы ЗАНЯТЫ и СПЯТ, по памяти - одним списком;
// карточка раскрывает процессы выбранного, закрыть можно бережно или принудительно.

// ProcRow: один процесс внутри группы.
type ProcRow struct {
	PID, Name, Note string
	CPU             float64
	RSS             int64
}

// ProcGroup: все процессы одного приложения или программы.
type ProcGroup struct {
	Name      string
	CPU       float64
	RSS       int64
	Age       time.Duration
	IsApp     bool // закрывается как Cmd+Q
	Protected bool // системный: не трогаем
	Procs     []ProcRow
}

// ProcsData: снимок машины.
type ProcsData struct {
	Groups            []ProcGroup
	CPU               int
	MemUsed, MemTotal int64
}

// ProcsOpts: источники экрана. Close ждёт закрытия до нескольких секунд - зовётся в фоне. Из беды
// памяти экран открывается по памяти, с курсором на держателе и текстом беды над списком.
type ProcsOpts struct {
	Load    func() ProcsData
	Close   func(g ProcGroup, force bool) (string, error)
	Query   string
	ByMem   bool
	Focus   string
	Warning string
}

type procsScreen struct {
	opts     ProcsOpts
	input    textinput.Model
	data     ProcsData
	shown    []ProcGroup
	list     List
	byMem    bool
	loading  bool
	closing  string
	expanded bool
	warning  string
	focus    string
	inTabs   bool
}

type (
	procsDataMsg ProcsData
	procsTickMsg struct{}
)

// NewProcs создаёт экран процессов.
func NewProcs(o ProcsOpts) Screen {
	in := textinput.New()
	in.Prompt = symPointer + " "
	in.Placeholder = "имя приложения или программы"
	in.SetWidth(40)
	st := in.Styles()
	st.Focused.Prompt = stylePrompt
	st.Focused.Placeholder = styleHint
	st.Cursor.Color = colAccent
	in.SetStyles(st)
	in.SetValue(o.Query)
	// Фильтр спит, пока не нажали "/": буквы - действия, а не текст. Иначе после
	// tim ps chrome нажатие k дописывало бы букву в фильтр вместо закрытия.
	in.Blur()
	return &procsScreen{opts: o, input: in, loading: true, byMem: o.ByMem, warning: o.Warning, focus: o.Focus}
}

func (p *procsScreen) setInTabs() { p.inTabs = true }

// TabName: вкладка Машины.
func (p *procsScreen) TabName() string {
	n := 0
	for _, g := range p.data.Groups {
		n += len(g.Procs)
	}
	if n == 0 {
		return "Процессы"
	}
	return "Процессы " + itoa(n)
}

func (p *procsScreen) SearchLine() string {
	if p.input.Focused() || p.input.Value() != "" {
		return p.input.View()
	}
	return styleHint.Render(symPointer + " найти")
}

func (p *procsScreen) OrderText() string {
	if p.byMem {
		return "по памяти"
	}
	return "по CPU"
}

// Escape: первый esc уходит из поиска, второй его очищает.
func (p *procsScreen) Escape() bool {
	switch {
	case p.input.Focused():
		p.input.Blur()
	case p.input.Value() != "":
		p.input.SetValue("")
		p.refill()
	default:
		return false
	}
	return true
}

func (p *procsScreen) Init() tea.Cmd { return tea.Batch(textinput.Blink, p.load(), p.tick()) }

func (p *procsScreen) load() tea.Cmd {
	return func() tea.Msg { return procsDataMsg(p.opts.Load()) }
}

func (p *procsScreen) tick() tea.Cmd {
	return tea.Tick(3*time.Second, func(time.Time) tea.Msg { return procsTickMsg{} })
}

func (p *procsScreen) Crumbs() []string { return []string{"Машина"} }

func (p *procsScreen) Status() string {
	if p.loading && len(p.data.Groups) == 0 {
		return spinFrames[0] + " смотрю"
	}
	return fmt.Sprintf("CPU %d%% %s память %s из %s", p.data.CPU, symSep, ui.Bytes(p.data.MemUsed), ui.Bytes(p.data.MemTotal))
}

func (p *procsScreen) Command() string {
	if cur := p.current(); cur != nil {
		return "tim kill " + strings.ToLower(cur.Name)
	}
	return "tim top"
}

func (p *procsScreen) Typing() bool { return p.input.Focused() }

func (p *procsScreen) Keys() []Key {
	if p.input.Focused() {
		return []Key{{"enter", "применить"}, {"esc", "закрыть фильтр"}}
	}
	return []Key{{"enter", "раскрыть"}, {"/", "найти"}, {"p", "порядок"}, {"r", "обновить"}}
}

func (p *procsScreen) refill() {
	q := strings.TrimSpace(p.input.Value())
	p.shown = nil
	if q == "" {
		p.shown = append(p.shown, p.data.Groups...)
	} else {
		names := make([]string, len(p.data.Groups))
		for i, g := range p.data.Groups {
			names[i] = g.Name
		}
		for _, m := range fuzzy.Filter(q, names) {
			if m.Score < fuzzy.MinScore(q) {
				continue
			}
			p.shown = append(p.shown, p.data.Groups[m.Index])
		}
	}
	// Стабильная сортировка: при обновлении раз в 3 с строки не должны прыгать от шума.
	slices.SortStableFunc(p.shown, p.compare)
	p.list.Rows = nil
	busy := 0
	for _, g := range p.shown {
		if g.CPU >= 1 {
			busy++
		}
	}
	for i, g := range p.shown {
		if !p.byMem && q == "" {
			switch {
			case i == 0 && g.CPU >= 1:
				p.list.Rows = append(p.list.Rows, headerRow("ЗАНЯТЫ "+symSep+" "+itoa(busy)))
			case g.CPU < 1 && (i == 0 || p.shown[i-1].CPU >= 1):
				p.list.Rows = append(p.list.Rows, headerRow("СПЯТ "+symSep+" "+itoa(len(p.shown)-busy)+" по памяти, меньше 1% CPU"))
			}
		}
		p.list.Rows = append(p.list.Rows, Row{Text: g.Name, Render: p.renderRow(g)})
	}
	p.list.Cursor = min(p.list.Cursor, max(0, len(p.list.Rows)-1))
	p.list.Skip(1)
}

// renderRow: имя, числа вправо по колонкам; у спящих клетка CPU пустая - это сказано в заголовке.
func (p *procsScreen) renderRow(g ProcGroup) func(bool, int) string {
	return func(sel bool, w int) string {
		name := styleText.Render(ui.Fit(g.Name, 28))
		if sel {
			name = styleBold.Render(ui.Fit(g.Name, 28))
		}
		cpu := pctLevel(int(g.CPU)).Render(fmt.Sprintf("%5.0f%%", g.CPU))
		switch {
		case g.CPU < 1 && !p.byMem:
			cpu = "      "
		case g.CPU < 1:
			cpu = styleDim.Render(fmt.Sprintf("%5.0f%%", g.CPU))
		}
		mem := fmt.Sprintf("%9s", ui.Bytes(g.RSS))
		count := fmt.Sprintf("%7d", len(g.Procs))
		if g.Protected {
			count = styleDim.Render(fmt.Sprintf("%7s", "сист."))
		}
		if p.byMem {
			return SelRow(sel, w, name+mem+"  "+cpu+count)
		}
		return SelRow(sel, w, name+cpu+mem+count)
	}
}

// compare: тяжёлые первыми - по памяти или по процессору, а при равном процессоре по памяти.
func (p *procsScreen) compare(a, b ProcGroup) int {
	if p.byMem {
		return cmp.Compare(b.RSS, a.RSS)
	}
	// Спящие - ниже занятых и по памяти: между ними разница в долях процента - шум.
	if (a.CPU >= 1) != (b.CPU >= 1) {
		return cmp.Compare(b.CPU, a.CPU)
	}
	if a.CPU < 1 {
		return cmp.Compare(b.RSS, a.RSS)
	}
	return cmp.Or(cmp.Compare(b.CPU, a.CPU), cmp.Compare(b.RSS, a.RSS))
}

func (p *procsScreen) current() *ProcGroup {
	n := -1
	for i, r := range p.list.Rows {
		if !r.Header {
			n++
		}
		if i == p.list.Cursor && !r.Header && n < len(p.shown) {
			return &p.shown[n]
		}
	}
	return nil
}

// selectName: курсор на приложение с этим именем.
func (p *procsScreen) selectName(name string) {
	for i, r := range p.list.Rows {
		if !r.Header && r.Text == name {
			p.list.Cursor = i
		}
	}
}

func (p *procsScreen) Update(msg tea.Msg, app *App) tea.Cmd {
	switch msg := msg.(type) {
	case procsDataMsg:
		p.data, p.loading = ProcsData(msg), false
		cur := ""
		if c := p.current(); c != nil {
			cur = c.Name
		}
		p.refill()
		// Курсор остаётся на том же приложении, даже если оно уехало по списку; в первый раз - на
		// держателе памяти, если экран открыт из беды.
		if p.focus != "" {
			cur, p.focus = p.focus, ""
		}
		p.selectName(cur)
		return nil
	case procsTickMsg:
		var cmd tea.Cmd
		if !p.loading && p.closing == "" {
			p.loading = true
			cmd = p.load()
		}
		return tea.Batch(cmd, p.tick())
	case JobDone:
		p.closing = ""
		p.loading = true
		return p.load()
	case tea.KeyPressMsg:
		k := msg.String()
		if p.input.Focused() {
			if k == "enter" {
				p.input.Blur()
				return nil
			}
			var cmd tea.Cmd
			before := p.input.Value()
			p.input, cmd = p.input.Update(msg)
			if p.input.Value() != before {
				p.refill()
			}
			return cmd
		}
		if p.list.Key(k, 10) {
			return nil
		}
		switch k {
		case "/":
			p.warning = ""
			return p.input.Focus()
		case "enter":
			p.expanded = !p.expanded
		case "p":
			p.byMem, p.warning = !p.byMem, ""
			cur := ""
			if c := p.current(); c != nil {
				cur = c.Name
			}
			p.refill()
			p.selectName(cur)
		// k занят списком (вверх), поэтому закрыть - x, как убрать на экранах диска.
		case "X":
			return p.confirmClose(app, true)
		case "x":
			return p.confirmClose(app, false)
		case "r":
			p.loading = true
			return p.load()
		}
		return nil
	}
	var cmd tea.Cmd
	p.input, cmd = p.input.Update(msg)
	return cmd
}

func (p *procsScreen) confirmClose(app *App, force bool) tea.Cmd {
	cur := p.current()
	switch {
	case cur == nil:
		return nil
	case cur.Protected:
		return app.Say(level.Warn, cur.Name+" - системный процесс, без него машина или сеанс упадут")
	}
	g := *cur
	d := Dialog{Title: "Закрыть " + g.Name + "?", OK: "Закрыть"}
	switch {
	case force:
		d.Title = "Убить " + g.Name + " принудительно?"
		d.Lines = []string{"SIGKILL " + ui.Count(len(g.Procs), "процессу", "процессам", "процессам") + ", несохранённое пропадёт."}
		d.OK, d.Danger = "Убить", true
	case g.IsApp:
		d.Lines = []string{"Как Cmd+Q: приложение само спросит про несохранённое и закроет окна.",
			"Не закроется за 8 с - X закроет принудительно."}
	default:
		d.Lines = []string{"SIGTERM " + ui.Count(len(g.Procs), "процессу", "процессам", "процессам") + ", потом ждём до 5 с."}
	}
	d.Yes = func() tea.Cmd {
		p.closing = g.Name
		return app.Job("закрываю "+g.Name, func() (string, error) { return p.opts.Close(g, force) })
	}
	return app.Confirm(d)
}
