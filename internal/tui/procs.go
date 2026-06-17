package tui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/TimurTurdyev/tim/internal/fuzzy"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// Процессы, сложенные по приложениям: фильтр сверху, панель раскрывает выбранное, закрыть
// можно бережно или принудительно - через общее окно подтверждения.

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

// ProcsOpts: источники экрана. Close ждёт закрытия до нескольких секунд - зовётся в фоне.
type ProcsOpts struct {
	Load  func() ProcsData
	Close func(g ProcGroup, force bool) (string, error)
	Query string
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
}

type (
	procsDataMsg ProcsData
	procsTickMsg struct{}
	closedMsg    struct {
		text string
		err  error
	}
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
	return &procsScreen{opts: o, input: in, loading: true}
}

func (p *procsScreen) Escape() bool {
	if !p.input.Focused() {
		return false
	}
	p.input.Blur()
	return true
}

func (p *procsScreen) Init() tea.Cmd { return tea.Batch(textinput.Blink, p.load(), p.tick()) }

func (p *procsScreen) load() tea.Cmd {
	return func() tea.Msg { return procsDataMsg(p.opts.Load()) }
}

func (p *procsScreen) tick() tea.Cmd {
	return tea.Tick(3*time.Second, func(time.Time) tea.Msg { return procsTickMsg{} })
}

func (p *procsScreen) Crumbs() []string { return []string{"Машина", "Процессы"} }

func (p *procsScreen) Status() string {
	switch {
	case p.closing != "":
		return "закрываю " + p.closing + "..."
	case p.loading && len(p.data.Groups) == 0:
		return "смотрю..."
	}
	return fmt.Sprintf("CPU %d%% %s память %s из %s", p.data.CPU, symDot, Human(p.data.MemUsed), Human(p.data.MemTotal))
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
	return []Key{{symUpDown, "выбор"}, {"x", "закрыть бережно"}, {"X", "принудительно"}, {"enter", "раскрыть"}, {"/", "фильтр"}, {"tab", "память/CPU"}, {"esc", "назад"}}
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
			if m.Score < len([]rune(q))*2 {
				continue
			}
			p.shown = append(p.shown, p.data.Groups[m.Index])
		}
	}
	// Стабильная сортировка: при обновлении раз в 3 с строки не должны прыгать от шума.
	for i := 1; i < len(p.shown); i++ {
		for j := i; j > 0 && p.less(p.shown[j], p.shown[j-1]); j-- {
			p.shown[j], p.shown[j-1] = p.shown[j-1], p.shown[j]
		}
	}
	p.list.Rows = nil
	for _, g := range p.shown {
		g := g
		p.list.Rows = append(p.list.Rows, Row{Text: g.Name, Render: func(sel bool, w int) string {
			name := styleText.Render(ui.Fit(ui.Clip(g.Name, 27), 28))
			if sel {
				name = styleBold.Render(ui.Fit(ui.Clip(g.Name, 27), 28))
			}
			cpu := pctLevel(int(g.CPU / 2)).Render(fmt.Sprintf("%5.0f%%", g.CPU))
			if g.CPU < 5 {
				cpu = styleDim.Render(fmt.Sprintf("%5.0f%%", g.CPU))
			}
			tail := styleDim.Render(fmt.Sprintf("  %-13s %s", plural(len(g.Procs), "процесс", "процесса", "процессов"), durShort(g.Age)))
			if g.Protected {
				tail = styleDim.Render("  системный")
			}
			return SelRow(sel, w, name+cpu+"  "+styleText.Render(ui.Fit(Human(g.RSS), 8))+tail)
		}})
	}
	if p.list.Cursor >= len(p.list.Rows) {
		p.list.Cursor = max(0, len(p.list.Rows)-1)
	}
}

func (p *procsScreen) less(a, b ProcGroup) bool {
	if p.byMem {
		return a.RSS > b.RSS
	}
	if a.CPU != b.CPU {
		return a.CPU > b.CPU
	}
	return a.RSS > b.RSS
}

func (p *procsScreen) current() *ProcGroup {
	if p.list.Cursor < len(p.shown) {
		return &p.shown[p.list.Cursor]
	}
	return nil
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
		// Курсор остаётся на том же приложении, даже если оно уехало по списку.
		for i, g := range p.shown {
			if g.Name == cur {
				p.list.Cursor = i
			}
		}
		return nil
	case procsTickMsg:
		var cmd tea.Cmd
		if !p.loading && p.closing == "" {
			p.loading = true
			cmd = p.load()
		}
		return tea.Batch(cmd, p.tick())
	case closedMsg:
		p.closing = ""
		p.loading = true
		if msg.err != nil {
			return tea.Batch(app.Toast("bad", msg.err.Error()), p.load())
		}
		return tea.Batch(app.Toast("ok", msg.text), p.load())
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
			return p.input.Focus()
		case "enter":
			p.expanded = !p.expanded
		case "tab":
			p.byMem = !p.byMem
			p.refill()
		// k занят списком (вверх), поэтому закрыть - x, как убрать на экранах диска.
		case "X":
			return p.confirmClose(app, true)
		case "x", "ч":
			return p.confirmClose(app, false)
		case "r", "к":
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
		return app.Toast("warn", cur.Name+" - системный процесс, без него машина или сеанс упадут")
	case p.closing != "":
		return app.Toast("warn", "подожди, закрываю "+p.closing)
	}
	g := *cur
	d := Dialog{Title: "Закрыть " + g.Name + "?", OK: "Закрыть"}
	switch {
	case force:
		d.Title = "Убить " + g.Name + " принудительно?"
		d.Lines = []string{"SIGKILL " + plural(len(g.Procs), "процессу", "процессам", "процессам") + ", несохранённое пропадёт."}
		d.OK, d.Danger = "Убить", true
	case g.IsApp:
		d.Lines = []string{"Как Cmd+Q: приложение само спросит про несохранённое и закроет окна.",
			"Не закроется за 8 с - X закроет принудительно."}
	default:
		d.Lines = []string{"SIGTERM " + plural(len(g.Procs), "процессу", "процессам", "процессам") + ", потом ждём до 5 с."}
	}
	d.Yes = func() tea.Cmd {
		p.closing = g.Name
		return func() tea.Msg {
			text, err := p.opts.Close(g, force)
			return closedMsg{text, err}
		}
	}
	return app.Confirm(d)
}

func (p *procsScreen) View(width, height int) string {
	order := "по CPU  " + symDot + "  tab память"
	if p.byMem {
		order = "по памяти  " + symDot + "  tab CPU"
	}
	filter := p.input.View()
	if !p.input.Focused() && p.input.Value() == "" {
		filter = styleHint.Render(symPointer + " / - фильтр по имени")
	}
	out := []string{"  " + ui.Fit(filter, 44) + "  " + styleHint.Render(order), ""}
	if p.loading && len(p.data.Groups) == 0 {
		out = append(out, "  "+styleEmpty.Render("смотрю, кто что занял..."))
		return strings.Join(out, "\n")
	}
	var pane []string
	if cur := p.current(); cur != nil {
		var rows []string
		limit := 3
		if p.expanded {
			limit = 12
		}
		for i, r := range cur.Procs {
			if i >= limit {
				rows = append(rows, styleDim.Render(fmt.Sprintf("ещё %d  %s  enter - раскрыть", len(cur.Procs)-limit, symDot)))
				break
			}
			line := styleDim.Render(fmt.Sprintf("%4.0f%%", r.CPU)) + "  " + ui.Fit(Human(r.RSS), 8) + "  " + ui.Fit(ui.Clip(r.Name, 40), 42) + styleDim.Render("pid "+r.PID)
			if r.Note != "" {
				line += "  " + styleAmber.Render(r.Note)
			}
			rows = append(rows, line)
		}
		if w := groupAdvice(*cur); w != "" {
			rows = append(rows, styleAmber.Render(symWarn+" "+w))
		}
		sub := plural(len(cur.Procs), "процесс", "процесса", "процессов") + " " + symDot + " " + durShort(cur.Age)
		pane = Pane(cur.Name, sub, rows, width-4)
	}
	listH := max(3, height-len(out)-len(pane)-1)
	out = append(out, p.list.View(width, listH)...)
	if len(p.shown) == 0 {
		out = append(out, "  "+styleDim.Render("ничего похожего не запущено"))
	}
	for len(out) < height-len(pane) {
		out = append(out, "")
	}
	for _, l := range pane {
		out = append(out, "  "+l)
	}
	return strings.Join(out, "\n")
}

// groupAdvice: что с этим приложением не так, словами.
func groupAdvice(g ProcGroup) string {
	switch {
	case g.CPU >= 100 && g.IsApp:
		return fmt.Sprintf("%s и %.0f%% CPU - обычно одна тяжёлая вкладка или задача: закрой её внутри, или приложение здесь", Human(g.RSS), g.CPU)
	case g.CPU >= 100:
		return fmt.Sprintf("%.0f%% CPU - занято больше ядра; если это не сборка или индексация, стоит закрыть", g.CPU)
	case g.RSS >= 4<<30:
		return Human(g.RSS) + " памяти - больше, чем обычно нужно; перезапуск вернёт память"
	}
	for _, r := range g.Procs {
		if r.Note != "" {
			return r.Note
		}
	}
	return ""
}
