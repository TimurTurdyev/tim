package tui

import (
	"math/rand/v2"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/TimurTurdyev/tim/internal/level"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// Первый запуск: где проекты, что нашлось, подключить ли к оболочке, и финал. Корневой экран с
// крупным маскотом - единственное его место в приложении; esc - позже, конфиг тогда не пишется.

// shellChoices: подключить, подключить со сводкой, пропустить.
var shellChoices = []struct{ name, note string }{
	{"подключить", "одна строка"},
	{"подключить со сводкой", "то же, и tim status в каждом новом окне терминала"},
	{"пропустить", "настрою сам: $ tim init --help"},
}

// RootChoice: папка-кандидат с кодом.
type RootChoice struct {
	Path, Note string
}

// FoundProject: клон, найденный под корнем.
type FoundProject struct {
	Name, Stack, Dir, Age string
	Checked               bool
}

// OnboardOpts: что мастеру нужно от раздела. Count, Scan и Save могут быть долгими - зовутся в фоне.
type OnboardOpts struct {
	Roots func() []RootChoice
	Count func(root string) string // сколько репозиториев в папке и когда менялся последний
	Scan  func(root string) []FoundProject
	Save  func(root string, chosen []FoundProject) (string, error)
	Shell func(motd bool) (string, error) // подключить к оболочке; motd - ещё и сводка в каждом окне
	// ShellLine и ShellFile: точная строка и куда она допишется.
	ShellLine, ShellFile string
	Home                 func() Screen // куда уйти после мастера
	Build                string        // сборка для лок-апа
	Config               string        // где будет конфиг
}

type onboard struct {
	opts   OnboardOpts
	step   int // 0 папки, 1 проекты, 2 оболочка, 3 финал
	roots  []RootChoice
	counts map[string]string
	list   List
	input  textinput.Model
	root   string
	found  []FoundProject
	busy   string
	saved  int
	shell  string // что стало с оболочкой; пусто - пропущена
	blink  bool
	shutAt int
}

type (
	onbRootsMsg []RootChoice
	onbCountMsg struct{ root, note string }
	onbFoundMsg []FoundProject
	onbDoneMsg  struct {
		text string
		err  error
	}
)

// NewOnboard создаёт мастер первого запуска.
func NewOnboard(o OnboardOpts) Screen {
	in := textinput.New()
	in.Prompt = symPointer + " "
	in.Placeholder = "~/code"
	in.SetWidth(50)
	st := in.Styles()
	st.Focused.Prompt = stylePrompt
	st.Focused.Placeholder = styleHint
	st.Cursor.Color = colAccent
	in.SetStyles(st)
	return &onboard{opts: o, input: in, busy: "ищу папки с кодом", counts: map[string]string{}}
}

// Animating: крупный маскот моргает, кадр "считаю" движется.
func (o *onboard) Animating() bool { return true }

func (o *onboard) Tick(frame int) {
	o.blink = frame == o.shutAt
	if frame > o.shutAt {
		o.shutAt = frame + 12 + rand.IntN(16)
	}
}

func (o *onboard) Init() tea.Cmd {
	return func() tea.Msg { return onbRootsMsg(o.opts.Roots()) }
}

func (o *onboard) Crumbs() []string { return nil }

func (o *onboard) Status() string { return "" }

func (o *onboard) Command() string { return "tim config init" }

func (o *onboard) Typing() bool { return o.input.Focused() }

func (o *onboard) Escape() bool {
	if o.input.Focused() {
		o.input.Blur()
		return true
	}
	if o.step == 1 {
		o.step--
		o.fillRoots()
		return true
	}
	return false
}

func (o *onboard) Keys() []Key {
	switch {
	case o.busy != "":
		return []Key{{"esc", "позже"}}
	case o.input.Focused():
		return []Key{{"enter", "выбрать папку"}, {"esc", "назад к списку"}}
	case o.step == 0:
		return []Key{{"enter", "дальше"}, {"esc", "позже"}}
	case o.step == 1:
		return []Key{{"enter", "сохранить"}, {"space", "отметить"}, {"v", "все"}, {"esc", "назад"}}
	case o.step == 2:
		return []Key{{"enter", "выполнить"}, {"esc", "позже"}}
	}
	return []Key{{"enter", "на главный"}, {"esc", "выход"}}
}

func (o *onboard) fillRoots() {
	o.list.Rows = nil
	for _, r := range o.roots {
		o.list.Rows = append(o.list.Rows, Row{Text: r.Path, Render: func(sel bool, w int) string {
			name := styleText.Render(ui.Fit(r.Path, 17))
			if sel {
				name = styleBold.Render(ui.Fit(r.Path, 17))
			}
			note, ok := o.counts[r.Path]
			if !ok {
				note = Spinner() + " считаю репозитории"
			}
			return SelRow(sel, w, name+styleDim.Render(note))
		}})
	}
	o.list.Rows = append(o.list.Rows, Row{Text: "другая", Render: func(sel bool, w int) string {
		return SelRow(sel, w, styleText.Render(ui.Fit("другая папка...", 17))+styleDim.Render("введи путь"))
	}})
	if o.list.Cursor >= len(o.list.Rows) {
		o.list.Cursor = 0
	}
}

func (o *onboard) fillFound() {
	o.list.Rows = nil
	for i := range o.found {
		o.list.Rows = append(o.list.Rows, Row{Text: o.found[i].Name, Render: func(sel bool, w int) string {
			p := o.found[i]
			mark := " "
			if p.Checked {
				mark = styleText.Render(symCheck)
			}
			name := styleText.Render(ui.Fit(p.Name, 26))
			if sel {
				name = styleBold.Render(ui.Fit(p.Name, 26))
			}
			return SelRow(sel, w, mark+"  "+name+styleDim.Render(ui.Fit(p.Stack, 11)+ui.Fit(p.Age, 10)+ui.Clip(p.Dir, max(10, w-58))))
		}})
	}
	o.list.Cursor = 0
}

func (o *onboard) chosen() []FoundProject {
	var out []FoundProject
	for _, p := range o.found {
		if p.Checked {
			out = append(out, p)
		}
	}
	return out
}

func (o *onboard) scan(root string) tea.Cmd {
	o.root = root
	o.busy = "ищу репозитории в " + root
	return func() tea.Msg { return onbFoundMsg(o.opts.Scan(root)) }
}

// count: репозитории каждой папки-кандидата считаются в фоне, по отдельности.
func (o *onboard) count() tea.Cmd {
	if o.opts.Count == nil {
		return nil
	}
	var cmds []tea.Cmd
	for _, r := range o.roots {
		root := r.Path
		cmds = append(cmds, func() tea.Msg { return onbCountMsg{root, o.opts.Count(root)} })
	}
	return tea.Batch(cmds...)
}

func (o *onboard) Update(msg tea.Msg, app *App) tea.Cmd {
	switch msg := msg.(type) {
	case onbRootsMsg:
		o.roots, o.busy = []RootChoice(msg), ""
		o.fillRoots()
		return o.count()
	case onbCountMsg:
		o.counts[msg.root] = msg.note
		return nil
	case onbFoundMsg:
		o.found, o.busy, o.step = []FoundProject(msg), "", 1
		o.fillFound()
		return nil
	case onbDoneMsg:
		o.busy = ""
		if msg.err != nil {
			return app.Say(level.Bad, msg.err.Error())
		}
		if o.step == 1 {
			o.saved, o.step = len(o.chosen()), 2
			o.fillShell()
			return nil
		}
		o.shell, o.step = msg.text, 3
		if msg.text != "" {
			return app.Say(level.OK, "конфиг: записан, оболочка подключена")
		}
		return app.Say(level.OK, "конфиг: записан")
	case tea.KeyPressMsg:
		if o.busy != "" {
			return nil
		}
		if o.input.Focused() {
			return o.typePath(msg, app)
		}
		switch k := msg.String(); o.step {
		case 0:
			return o.pickRoot(k)
		case 1:
			return o.pickProjects(k)
		case 2:
			return o.askShell(k)
		case 3:
			if k == "enter" && o.opts.Home != nil {
				return app.Replace(o.opts.Home())
			}
		}
	}
	return nil
}

// typePath: свой путь к папке с проектами вместо найденных.
func (o *onboard) typePath(msg tea.KeyPressMsg, app *App) tea.Cmd {
	if msg.String() == "enter" {
		path := strings.TrimSpace(o.input.Value())
		if path == "" {
			return app.Say(level.Warn, "путь: введи папку с проектами")
		}
		o.input.Blur()
		return o.scan(path)
	}
	var cmd tea.Cmd
	o.input, cmd = o.input.Update(msg)
	return cmd
}

// pickRoot: шаг 1 - где лежат проекты; последняя строка - ввести путь самому.
func (o *onboard) pickRoot(k string) tea.Cmd {
	if o.list.Key(k, 10) || k != "enter" {
		return nil
	}
	if o.list.Cursor >= len(o.roots) {
		return o.input.Focus()
	}
	return o.scan(o.roots[o.list.Cursor].Path)
}

// pickProjects: шаг 2 - какие из найденных проектов записать в конфиг.
func (o *onboard) pickProjects(k string) tea.Cmd {
	if o.list.Key(k, 10) {
		return nil
	}
	switch k {
	case "space", " ":
		if o.list.Cursor < len(o.found) {
			o.found[o.list.Cursor].Checked = !o.found[o.list.Cursor].Checked
			o.list.Down()
		}
	case "v":
		all := len(o.chosen()) == len(o.found)
		for i := range o.found {
			o.found[i].Checked = !all
		}
	case "enter":
		chosen, root := o.chosen(), o.root
		o.busy = "записываю конфиг"
		return func() tea.Msg {
			text, err := o.opts.Save(root, chosen)
			return onbDoneMsg{text, err}
		}
	}
	return nil
}

// askShell: шаг 3 - подключить ли tim к оболочке.
func (o *onboard) askShell(k string) tea.Cmd {
	if o.list.Key(k, 3) || k != "enter" {
		return nil
	}
	if o.list.Cursor == 2 || o.opts.Shell == nil {
		o.shell = ""
		return func() tea.Msg { return onbDoneMsg{} }
	}
	motd := o.list.Cursor == 1
	o.busy = "подключаю к оболочке"
	return func() tea.Msg {
		text, err := o.opts.Shell(motd)
		return onbDoneMsg{text, err}
	}
}

func (o *onboard) fillShell() {
	o.list.Rows = nil
	choices := shellChoices
	if o.opts.Shell == nil {
		choices = shellChoices[2:] // к этой оболочке tim не подключается - остаётся только пропуск
	}
	for i, c := range choices {
		note := c.note
		if i == 0 {
			note = o.opts.ShellFile + " " + symSep + " " + c.note
		}
		o.list.Rows = append(o.list.Rows, Row{Text: c.name, Render: func(sel bool, w int) string {
			name := styleText.Render(ui.Fit(c.name, 26))
			if sel {
				name = styleBold.Render(ui.Fit(c.name, 26))
			}
			return SelRow(sel, w, name+styleDim.Render(note))
		}})
	}
	o.list.Cursor = 0
}
