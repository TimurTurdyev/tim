package tui

import (
	"slices"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/TimurTurdyev/tim/internal/fuzzy"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// Проекты, вкладка Все: недавние сверху и остальные по имени, поиск, лампа запущенного из скана портов,
// карточка выбранного справа или под списком с действиями капами.

// Колонки списка проектов.
const (
	projNameW = 27
	projAgeW  = 6
	projWideW = 160 // от стольких колонок - ещё и колонка ПАПКА
)

// ProjectRow: строка списка проектов.
type ProjectRow struct {
	Name    string
	Dir     string
	Folder  string // папка относительно корня проектов - для широкого окна
	Age     string // давность захода; пусто - не заходил
	Visited bool
	Port    string // слушает dev-сервер; пусто - не запущен
}

// ProjectAction: действие над проектом - кап в карточке. Run - в фоне со строкой состояния, Do -
// открыть экран или закрыть tim; Label и When могут зависеть от проекта.
type ProjectAction struct {
	Key, Label, Note string
	LabelFor         func(p ProjectRow) string
	When             func(p ProjectRow) bool
	Doing            func(p ProjectRow) string
	Do               func(p ProjectRow) Action
	Run              func(p ProjectRow) (string, error)
}

// ProjectsOpts: источники вкладки проектов.
type ProjectsOpts struct {
	Rows    []ProjectRow
	Card    func(p ProjectRow) Card // факты; считается при выборе, может быть медленным
	Actions []ProjectAction
	Go      func(p ProjectRow) Action // enter
	Running func() map[string]string  // проект -> порт по скану портов; зовётся в фоне
	Empty   []Key                     // клавиши пустого реестра
	OnEmpty func(key string) Action
}

type projects struct {
	opts     ProjectsOpts
	input    textinput.Model
	list     List
	shown    []ProjectRow
	byName   bool
	cards    map[string]Card
	running  map[string]string
	scanning bool
}

type (
	cardMsg struct {
		name string
		card Card
	}
	runningMsg map[string]string
)

// NewProjects создаёт вкладку проектов.
func NewProjects(opts ProjectsOpts) Tab {
	in := textinput.New()
	in.Prompt = symPointer + " "
	in.Placeholder = "найти"
	in.SetWidth(30)
	st := in.Styles()
	st.Focused.Prompt = stylePrompt
	st.Focused.Placeholder = styleHint
	st.Cursor.Color = colAccent
	in.SetStyles(st)
	// Поиск спит, пока не нажали "/": буквы - действия над проектом.
	in.Blur()
	p := &projects{opts: opts, input: in, cards: map[string]Card{}}
	p.refill()
	return p
}

func (p *projects) Init() tea.Cmd {
	var scan tea.Cmd
	if p.opts.Running != nil {
		p.scanning = true
		scan = func() tea.Msg { return runningMsg(p.opts.Running()) }
	}
	return tea.Batch(textinput.Blink, p.loadCard(), scan)
}

func (p *projects) Crumbs() []string { return []string{"Проекты"} }

func (p *projects) TabName() string { return "Все " + itoa(len(p.opts.Rows)) }

func (p *projects) Status() string {
	n := ui.Count(len(p.opts.Rows), "проект", "проекта", "проектов")
	if p.scanning {
		return spinFrames[0] + " " + n + " " + symSep + " ищу порты"
	}
	return n + " " + symSep + " " + ui.Count(len(p.running), "запущен", "запущено", "запущено")
}

func (p *projects) Command() string {
	if cur := p.current(); cur != nil {
		return "tim go " + cur.Name
	}
	return "tim go"
}

func (p *projects) Typing() bool { return p.input.Focused() }

// Escape: первый esc уходит из поиска, второй его очищает, третий - назад.
func (p *projects) Escape() bool {
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

func (p *projects) Cursor() *tea.Cursor { return nil }

func (p *projects) SearchLine() string {
	if p.input.Focused() || p.input.Value() != "" {
		return p.input.View()
	}
	return styleHint.Render(symPointer + " найти")
}

func (p *projects) OrderText() string {
	switch {
	case strings.TrimSpace(p.input.Value()) != "":
		return "по совпадению"
	case p.byName:
		return "по имени"
	}
	return "недавние сверху"
}

func (p *projects) Keys() []Key {
	switch {
	case p.input.Focused():
		return []Key{{"enter", "перейти"}, {"esc", "очистить"}}
	case len(p.opts.Rows) == 0:
		return p.opts.Empty
	}
	return []Key{{"enter", "перейти"}, {"/", "найти"}, {"p", "порядок"}}
}

// refill: недавние сверху и остальные по имени; при поиске - найденное по совпадению.
func (p *projects) refill() {
	q := strings.TrimSpace(p.input.Value())
	rows := slices.Clone(p.opts.Rows)
	for i := range rows {
		rows[i].Port = p.running[rows[i].Name]
	}
	p.shown = nil
	var groups []string
	switch {
	case q != "":
		names := make([]string, len(rows))
		for i, r := range rows {
			names[i] = r.Name
		}
		for _, m := range fuzzy.Filter(q, names) {
			p.shown = append(p.shown, rows[m.Index])
		}
		groups = []string{"НАЙДЕНО " + symSep + " " + itoa(len(p.shown)) + " из " + itoa(len(rows))}
	case p.byName:
		p.shown = rows
		slices.SortStableFunc(p.shown, func(a, b ProjectRow) int { return strings.Compare(a.Name, b.Name) })
	default:
		var recent, rest []ProjectRow
		for _, r := range rows {
			if r.Visited {
				recent = append(recent, r)
			} else {
				rest = append(rest, r)
			}
		}
		slices.SortStableFunc(rest, func(a, b ProjectRow) int { return strings.Compare(a.Name, b.Name) })
		p.shown = slices.Concat(recent, rest)
		if len(recent) > 0 {
			groups = []string{"НЕДАВНИЕ " + symSep + " " + itoa(len(recent)), "ОСТАЛЬНЫЕ " + symSep + " " + itoa(len(rest)) + " по имени"}
		}
	}
	p.list.Rows = nil
	for i, r := range p.shown {
		switch {
		case len(groups) == 1 && i == 0:
			p.list.Rows = append(p.list.Rows, headerRow(groups[0]))
		case len(groups) == 2 && i == 0 && r.Visited:
			p.list.Rows = append(p.list.Rows, headerRow(groups[0]))
		case len(groups) == 2 && !r.Visited && (i == 0 || p.shown[i-1].Visited):
			p.list.Rows = append(p.list.Rows, headerRow(groups[1]))
		}
		p.list.Rows = append(p.list.Rows, Row{Text: r.Name, Render: p.renderRow(r)})
	}
	p.list.Cursor = min(p.list.Cursor, max(0, len(p.list.Rows)-1))
	p.list.Skip(1)
}

func headerRow(text string) Row {
	return Row{Header: true, Text: text, Render: func(bool, int) string { return "   " + styleHead.Render(text) }}
}

func (p *projects) renderRow(r ProjectRow) func(bool, int) string {
	return func(sel bool, w int) string {
		name := styleText.Render(ui.Fit(r.Name, projNameW))
		if sel {
			name = styleBold.Render(ui.Fit(r.Name, projNameW))
		}
		line := name + styleDim.Render(padLeft(r.Age, projAgeW)) + "     "
		dev := ""
		if r.Port != "" {
			dev = styleGood.Render(symLamp + " :" + r.Port)
		}
		line += ui.Pad(dev, 12)
		if w >= 80 && r.Folder != "" {
			line += styleDim.Render(r.Folder)
		}
		return SelRow(sel, w, line)
	}
}

func padLeft(s string, w int) string { return strings.Repeat(" ", max(0, w-ui.Width(s))) + s }

func (p *projects) current() *ProjectRow {
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

func (p *projects) loadCard() tea.Cmd {
	cur := p.current()
	if cur == nil || p.opts.Card == nil {
		return nil
	}
	if _, ok := p.cards[cur.Name]; ok {
		return nil
	}
	row := *cur
	return func() tea.Msg { return cardMsg{row.Name, p.opts.Card(row)} }
}

func (p *projects) Update(msg tea.Msg, app *App) tea.Cmd {
	switch msg := msg.(type) {
	case cardMsg:
		p.cards[msg.name] = msg.card
		return nil
	case runningMsg:
		p.running, p.scanning = map[string]string(msg), false
		p.refill()
		return nil
	case JobDone:
		// После запуска или остановки сервера карточка и лампы устарели.
		if cur := p.current(); cur != nil {
			delete(p.cards, cur.Name)
		}
		return tea.Batch(p.Init(), p.loadCard())
	case tea.KeyPressMsg:
		return p.key(msg, app)
	}
	return nil
}

func (p *projects) key(msg tea.KeyPressMsg, app *App) tea.Cmd {
	k := msg.String()
	if len(p.opts.Rows) == 0 && p.opts.OnEmpty != nil {
		return app.Do(p.opts.OnEmpty(k))
	}
	if p.input.Focused() {
		switch k {
		case "enter":
			p.input.Blur()
			if cur := p.current(); cur != nil && p.opts.Go != nil {
				return app.Do(p.opts.Go(*cur))
			}
			return nil
		case "up", "down", "pgup", "pgdown":
			p.list.Key(k, 10)
			return p.loadCard()
		}
		var cmd tea.Cmd
		before := p.input.Value()
		p.input, cmd = p.input.Update(msg)
		if p.input.Value() != before {
			p.refill()
			return tea.Batch(cmd, p.loadCard())
		}
		return cmd
	}
	if p.list.Key(k, 10) {
		return p.loadCard()
	}
	cur := p.current()
	switch k {
	case "/":
		return p.input.Focus()
	case "p":
		p.byName = !p.byName
		p.refill()
		return p.loadCard()
	case "enter":
		if cur != nil && p.opts.Go != nil {
			return app.Do(p.opts.Go(*cur))
		}
		return nil
	}
	if cur == nil {
		return nil
	}
	for _, a := range p.actions(*cur) {
		if a.Key != k {
			continue
		}
		if a.Run != nil {
			doing := a.Label
			if a.Doing != nil {
				doing = a.Doing(*cur)
			}
			row := *cur
			return app.Job(doing, func() (string, error) { return a.Run(row) })
		}
		return app.Do(a.Do(*cur))
	}
	return nil
}

// actions: действия, которые есть у этого проекта, с подписью под его состояние.
func (p *projects) actions(r ProjectRow) []ProjectAction {
	var out []ProjectAction
	for _, a := range p.opts.Actions {
		if a.When != nil && !a.When(r) {
			continue
		}
		if a.LabelFor != nil {
			a.Label = a.LabelFor(r)
		}
		out = append(out, a)
	}
	return out
}

func (p *projects) card() *Card {
	cur := p.current()
	if cur == nil {
		return nil
	}
	c, ok := p.cards[cur.Name]
	if !ok {
		c = Card{Title: cur.Name, Path: ui.Dim(cur.Dir), Loading: true}
	}
	c.Enter = "перейти в папку, tim закроется"
	for _, a := range p.actions(*cur) {
		c.Actions = append(c.Actions, CardAction{a.Key, a.Label, a.Note})
	}
	return &c
}

func (p *projects) View(width, height int) string {
	out := []string{""}
	if len(p.opts.Rows) == 0 {
		return strings.Join(emptyRegistry(width), "\n")
	}
	wide := width >= projWideW
	head := ui.Fit("ПРОЕКТ", projNameW) + ui.Fit("ЗАХОДИЛ "+sortMark(!p.byName), 11) + "DEV-СЕРВЕР"
	if wide {
		head = ui.Fit(head, projNameW+11+12) + "ПАПКА"
	}
	out = append(out, "   "+styleHead.Render(head))
	if len(p.shown) == 0 {
		out = append(out, "   "+styleDim.Render("ничего похожего на \""+strings.TrimSpace(p.input.Value())+"\" из "+itoa(len(p.opts.Rows))))
		return strings.Join(out, "\n")
	}
	room := height - len(out)
	cw, beside := cardWidth(width)
	card := p.card()
	listW := cardListW
	if wide {
		listW = min(92, width-cw-cardGap-3)
	}
	switch {
	case card != nil && beside:
		list := p.list.View(listW, room)
		lines := card.Lines(cw, room)
		// Карточка начинается на строке шапки колонок: её линия вровень с ней.
		out = out[:len(out)-1]
		list = append([]string{"   " + styleHead.Render(head)}, list...)
		out = append(out, besideListW(list, lines, listW, width)...)
	case card != nil:
		cardH := min(len(card.Lines(cw, room)), max(5, room/3))
		out = append(out, p.list.View(width, room-cardH-1)...)
		out = append(out, "")
		for _, l := range card.Lines(cw, cardH) {
			out = append(out, "   "+l)
		}
	default:
		out = append(out, p.list.View(width, room)...)
	}
	return strings.Join(out, "\n")
}

// sortMark: знак сортировки у колонки, по которой отсортировано.
func sortMark(on bool) string {
	if on {
		return "\u25be"
	}
	return ""
}

// emptyRegistry: реестр пуст - что это и что делать.
func emptyRegistry(width int) []string {
	return Notice("Реестр пока пуст",
		styleDim.Render("tim знает о проекте только то, что в реестре: папка, стек, порт, база, сервер"),
		"найти репозитории в папке с кодом и отметить нужные, или добавить текущую папку", width)
}

func itoa(n int) string { return strconv.Itoa(n) }
