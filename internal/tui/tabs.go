package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/TimurTurdyev/tim/internal/ui"
)

// Вкладки: один экран из нескольких, tab и shift+tab листают. Вторая строка тела - вкладки слева,
// поиск вкладки в середине, порядок справа. Крошки - только экран: вкладку видно подсветкой.

// Tab: вкладка - экран без своей строки вкладок. TabName - название со счётчиком ("Все 62").
type Tab interface {
	Screen
	TabName() string
}

// tabSearch: у вкладки есть поиск - его строка встаёт в строку вкладок.
type tabSearch interface {
	SearchLine() string
}

// tabOrder: вкладка показывает, как отсортирована.
type tabOrder interface {
	OrderText() string
}

type tabMsg struct {
	tab int
	msg tea.Msg
}

type tabs struct {
	crumbs []string
	list   []Tab
	active int
	inited []bool
}

// inTabs: вкладке сказать, что строку поиска рисует строка вкладок.
type inTabs interface{ setInTabs() }

// NewTabs: экран из вкладок с этими крошками; start - какая открыта первой.
func NewTabs(crumbs []string, list []Tab, start int) Screen {
	for _, t := range list {
		if in, ok := t.(inTabs); ok {
			in.setInTabs()
		}
	}
	return &tabs{crumbs: crumbs, list: list, active: max(0, min(start, len(list)-1)), inited: make([]bool, len(list))}
}

func (t *tabs) cur() Tab { return t.list[t.active] }

func (t *tabs) Init() tea.Cmd { return t.open(t.active) }

// open: вкладка запускает свою загрузку при первом показе, а не все сразу.
func (t *tabs) open(i int) tea.Cmd {
	if t.inited[i] {
		return nil
	}
	t.inited[i] = true
	if in, ok := t.list[i].(Initer); ok {
		return tagTab(i, in.Init())
	}
	return nil
}

func (t *tabs) Crumbs() []string { return t.crumbs }
func (t *tabs) Status() string   { return t.cur().Status() }
func (t *tabs) Command() string  { return t.cur().Command() }

func (t *tabs) Keys() []Key {
	keys := t.cur().Keys()
	if !t.Typing() {
		keys = append(keys, Key{"tab", "вкладка"})
	}
	return keys
}

func (t *tabs) Typing() bool {
	ty, ok := t.cur().(Typer)
	return ok && ty.Typing()
}

func (t *tabs) Escape() bool {
	e, ok := t.cur().(Escaper)
	return ok && e.Escape()
}

func (t *tabs) Cursor() *tea.Cursor {
	c, ok := t.cur().(cursored)
	if !ok {
		return nil
	}
	cur := c.Cursor()
	if cur != nil {
		cur.Y++
	}
	return cur
}

func (t *tabs) Animating() bool {
	an, ok := t.cur().(animated)
	return ok && an.Animating()
}

func (t *tabs) Tick(frame int) {
	if an, ok := t.cur().(animated); ok {
		an.Tick(frame)
	}
}

// Switch: открыть вкладку i - например, из перехода с главного экрана.
func (t *tabs) Switch(i int) tea.Cmd {
	t.active = max(0, min(i, len(t.list)-1))
	return t.open(t.active)
}

func (t *tabs) Update(msg tea.Msg, app *App) tea.Cmd {
	switch m := msg.(type) {
	case tabMsg:
		return tagTab(m.tab, t.list[m.tab].Update(m.msg, app))
	case tea.KeyPressMsg:
		if !t.Typing() {
			switch m.String() {
			case "tab":
				return t.Switch((t.active + 1) % len(t.list))
			case "shift+tab":
				return t.Switch((t.active + len(t.list) - 1) % len(t.list))
			}
		}
		return tagTab(t.active, t.cur().Update(msg, app))
	case JobDone:
		return tagTab(t.active, t.cur().Update(msg, app))
	}
	var cmds []tea.Cmd
	for i, tab := range t.list {
		if t.inited[i] {
			cmds = append(cmds, tagTab(i, tab.Update(msg, app)))
		}
	}
	return tea.Batch(cmds...)
}

func (t *tabs) View(width, height int) string {
	return t.Line(width) + "\n" + t.cur().View(width, height-1)
}

// Line: строка вкладок - активная на бирюзовом, остальные серым; поиск и порядок активной.
func (t *tabs) Line(width int) string {
	left := "  "
	for i, tab := range t.list {
		name := " " + tab.TabName() + " "
		if i == t.active {
			left += styleTabOn.Render(name)
		} else {
			left += styleTabOff.Render(name)
		}
		left += " "
	}
	if s, ok := t.cur().(tabSearch); ok {
		if line := s.SearchLine(); line != "" {
			left += "    " + line
		}
	}
	right := ""
	if o, ok := t.cur().(tabOrder); ok {
		right = styleDim.Render(o.OrderText()) + " "
	}
	if ui.Width(left)+ui.Width(right)+2 > width {
		right = ""
	}
	return ui.Clip(left, width) + strings.Repeat(" ", max(0, width-ui.Width(left)-ui.Width(right))) + right
}

// tagTab помечает ответы команды вкладки её номером, как own - экранами.
func tagTab(i int, c tea.Cmd) tea.Cmd {
	if c == nil {
		return nil
	}
	return func() tea.Msg {
		switch m := c().(type) {
		case nil, tabMsg:
			return m
		case tea.BatchMsg:
			out := make(tea.BatchMsg, len(m))
			for j, cc := range m {
				out[j] = tagTab(i, cc)
			}
			return out
		default:
			if runtimeMsg(m) || appMsg(m) {
				return m
			}
			return tabMsg{tab: i, msg: m}
		}
	}
}
