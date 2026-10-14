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

// Экран проектов: список с поиском слева, карточка выбранного в рамке справа, действия буквами.

// ProjectRow: строка списка проектов.
type ProjectRow struct {
	Name    string
	Running bool
	Port    string
	Stack   string
	Branch  string
	Age     string
	Dir     string
}

// ProjectCard: что показать в панели; считается по выбору, может быть медленным.
type ProjectCard struct {
	Lines []string // уже с цветом
}

// ProjectAction: действие по букве.
type ProjectAction struct {
	Key, Label string
	Do         func(p ProjectRow) Action
}

// ProjectsOpts: источники экрана проектов.
type ProjectsOpts struct {
	Rows    []ProjectRow
	Card    func(p ProjectRow) ProjectCard
	Actions []ProjectAction
	Go      func(p ProjectRow) Action // enter
}

type projects struct {
	opts    ProjectsOpts
	input   textinput.Model
	list    List
	shown   []ProjectRow
	byName  bool
	cards   map[string]ProjectCard
	loading string
}

type cardMsg struct {
	name string
	card ProjectCard
}

// NewProjects создаёт экран проектов.
func NewProjects(opts ProjectsOpts) Screen {
	in := textinput.New()
	in.Prompt = symPointer + " "
	in.Placeholder = "имя проекта"
	in.SetWidth(40)
	st := in.Styles()
	st.Focused.Prompt = stylePrompt
	st.Focused.Placeholder = styleHint
	st.Cursor.Color = colAccent
	in.SetStyles(st)
	// Фильтр спит, пока не нажали "/": буквы - действия над проектом. Иначе первая буква имени
	// запускала бы действие: s у simple-cart поднимал сервер верхнего проекта.
	in.Blur()
	p := &projects{opts: opts, input: in, cards: map[string]ProjectCard{}}
	p.refill()
	return p
}

func (p *projects) Init() tea.Cmd { return tea.Batch(textinput.Blink, p.loadCard()) }

func (p *projects) Crumbs() []string { return []string{"Проекты"} }

func (p *projects) Status() string {
	running := 0
	for _, r := range p.opts.Rows {
		if r.Running {
			running++
		}
	}
	return ui.Count(len(p.opts.Rows), "проект", "проекта", "проектов") + " " + symDot + " " + ui.Count(running, "запущен", "запущено", "запущено")
}

func (p *projects) Command() string {
	if cur := p.current(); cur != nil {
		return "tim go " + cur.Name
	}
	return "tim go"
}

func (p *projects) Typing() bool { return p.input.Focused() }

// Escape: первый esc уходит из фильтра, второй очищает его, третий - назад.
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

func (p *projects) Keys() []Key {
	if p.input.Focused() {
		return []Key{{symUpDown, "выбор"}, {"enter", "перейти"}, {"esc", "готово"}}
	}
	k := []Key{{symUpDown, "выбор"}, {"enter", "перейти"}, {"/", "найти"}}
	for _, a := range p.opts.Actions {
		k = append(k, Key{a.Key, a.Label})
	}
	k = append(k, Key{"n", "по имени"}, Key{"esc", "назад"})
	return k
}

func (p *projects) refill() {
	q := strings.TrimSpace(p.input.Value())
	p.shown = nil
	if q == "" {
		p.shown = append(p.shown, p.opts.Rows...)
	} else {
		names := make([]string, len(p.opts.Rows))
		for i, r := range p.opts.Rows {
			names[i] = r.Name
		}
		for _, m := range fuzzy.Filter(q, names) {
			p.shown = append(p.shown, p.opts.Rows[m.Index])
		}
	}
	if p.byName {
		sortRows(p.shown)
	}
	nameW := 10
	for _, r := range p.opts.Rows {
		nameW = max(nameW, ui.Width(r.Name))
	}
	nameW = min(nameW, 24)
	p.list.Rows = nil
	for _, r := range p.shown {
		r := r
		p.list.Rows = append(p.list.Rows, Row{Text: r.Name, Render: func(sel bool, w int) string {
			name := styleText.Render(ui.Fit(r.Name, nameW))
			if sel {
				name = styleBold.Render(ui.Fit(r.Name, nameW))
			}
			line := name + "  " + styleDim.Render(ui.Fit(r.Stack, 10)+ui.Fit(r.Age, 7))
			// Лампа только у запущенных: столбец пустых кружков ничего не говорит.
			if r.Running {
				line += styleGood.Render(symLamp + " :" + r.Port)
			}
			return SelRow(sel, w, line)
		}})
	}
	if p.list.Cursor >= len(p.list.Rows) {
		p.list.Cursor = max(0, len(p.list.Rows)-1)
	}
}

func sortRows(rows []ProjectRow) {
	slices.SortStableFunc(rows, func(a, b ProjectRow) int { return strings.Compare(a.Name, b.Name) })
}

func (p *projects) current() *ProjectRow {
	if p.list.Cursor < len(p.shown) {
		return &p.shown[p.list.Cursor]
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
	p.loading = row.Name
	return func() tea.Msg { return cardMsg{row.Name, p.opts.Card(row)} }
}

func (p *projects) Update(msg tea.Msg, app *App) tea.Cmd {
	switch msg := msg.(type) {
	case cardMsg:
		p.cards[msg.name] = msg.card
		if p.loading == msg.name {
			p.loading = ""
		}
		return nil
	case tea.KeyPressMsg:
		k := msg.String()
		if p.list.Key(k, 10) && (!p.Typing() || len(k) != 1) {
			return p.loadCard()
		}
		if k == "/" && !p.Typing() {
			return p.input.Focus()
		}
		switch k {
		case "enter":
			if cur := p.current(); cur != nil && p.opts.Go != nil {
				return app.Do(p.opts.Go(*cur))
			}
			return nil
		case "n", "т":
			if !p.Typing() {
				p.byName = !p.byName
				p.refill()
				return p.loadCard()
			}
		}
		if !p.Typing() || len(k) > 1 {
			for _, a := range p.opts.Actions {
				if a.Key == k {
					if cur := p.current(); cur != nil {
						return app.Do(a.Do(*cur))
					}
				}
			}
		}
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

func (p *projects) View(width, height int) string {
	order := "недавние сверху  " + symDot + "  n по имени"
	if p.byName {
		order = "по имени  " + symDot + "  n недавние сверху"
	}
	filter := p.input.View()
	if !p.input.Focused() && p.input.Value() == "" {
		filter = styleHint.Render(symPointer + " / - найти по имени")
	}
	left := []string{"  " + filter, "  " + styleHint.Render(order), ""}
	listH := max(3, height-len(left)-2)
	rows := p.list.View(53, listH)
	left = append(left, rows...)

	var card []string
	if cur := p.current(); cur != nil {
		c, ok := p.cards[cur.Name]
		var body []string
		if ok {
			body = append(body, c.Lines...)
		} else {
			body = append(body, styleEmpty.Render("смотрю..."))
		}
		var actions []PaneSection
		if len(p.opts.Actions) > 0 {
			actions = []PaneSection{{Label: "действия", Rows: p.actionRows()}}
		}
		paneW := width - 54
		if width >= 100 {
			card = Pane(cur.Name, "", body, paneW, actions...)
		} else {
			// Узко: карточка под списком.
			left = append(left, "")
			left = append(left, Pane(cur.Name, "", body, width-4, actions...)...)
		}
	}
	if len(card) > 0 {
		return strings.Join(Beside(left, 53, card, width), "\n")
	}
	return strings.Join(left, "\n")
}

// actionRows: клавиши действий по три в строку.
func (p *projects) actionRows() []string {
	var rows []string
	line := ""
	for i, a := range p.opts.Actions {
		line += ui.Fit(styleKeyCap.Render(" "+a.Key+" ")+" "+styleText.Render(a.Label), 14)
		if (i+1)%3 == 0 || i == len(p.opts.Actions)-1 {
			rows = append(rows, line)
			line = ""
		}
	}
	return rows
}

func itoa(n int) string { return strconv.Itoa(n) }
