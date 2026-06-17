package tui

import (
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/TimurTurdyev/tim/internal/ui"
)

// Первый запуск: где проекты, что нашлось, подключить ли к оболочке. Три шага, каждый можно
// пропустить - esc закрывает мастер, конфиг тогда не пишется.

// RootChoice: папка-кандидат с кодом.
type RootChoice struct {
	Path, Note string
}

// FoundProject: клон, найденный под корнем.
type FoundProject struct {
	Name, Stack, Dir, Age string
	Checked               bool
}

// OnboardOpts: что мастеру нужно от cmd. Scan и Save могут быть долгими - зовутся в фоне.
type OnboardOpts struct {
	Roots func() []RootChoice
	Scan  func(root string) []FoundProject
	Save  func(root string, chosen []FoundProject) (string, error)
	Shell func() (string, error) // подключить к оболочке; пусто - уже подключено
	Home  func() Screen          // куда уйти после мастера
	Art   []string
}

type onboard struct {
	opts    OnboardOpts
	step    int
	roots   []RootChoice
	list    List
	input   textinput.Model
	root    string
	found   []FoundProject
	busy    string
	saved   string
	shellOK string
}

type (
	onbRootsMsg []RootChoice
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
	return &onboard{opts: o, input: in, busy: "ищу папки с кодом"}
}

func (o *onboard) Init() tea.Cmd {
	return func() tea.Msg { return onbRootsMsg(o.opts.Roots()) }
}

func (o *onboard) Crumbs() []string { return []string{"Первый запуск"} }

func (o *onboard) Status() string {
	if o.busy != "" {
		return o.busy + "..."
	}
	return "шаг " + itoa(o.step+1) + " из 3"
}

func (o *onboard) Command() string { return "tim config init" }

func (o *onboard) Typing() bool { return o.input.Focused() }

func (o *onboard) Escape() bool {
	if o.input.Focused() {
		o.input.Blur()
		return true
	}
	if o.step > 0 && o.step < 2 {
		o.step--
		o.fillRoots()
		return true
	}
	return false
}

func (o *onboard) Keys() []Key {
	switch {
	case o.input.Focused():
		return []Key{{"enter", "выбрать папку"}, {"esc", "назад к списку"}}
	case o.step == 0:
		return []Key{{symUpDown, "выбор"}, {"enter", "дальше"}, {"esc", "позже"}}
	case o.step == 1:
		return []Key{{"space", "галочка"}, {"a", "все"}, {"enter", "сохранить"}, {"esc", "назад"}}
	}
	return []Key{{"y", "подключить"}, {"n", "пропустить"}}
}

func (o *onboard) fillRoots() {
	o.list.Rows = nil
	for _, r := range o.roots {
		r := r
		o.list.Rows = append(o.list.Rows, Row{Text: r.Path, Render: func(sel bool, w int) string {
			name := styleText.Render(ui.Fit(r.Path, 22))
			if sel {
				name = styleBold.Render(ui.Fit(r.Path, 22))
			}
			return SelRow(sel, w, name+styleDim.Render(r.Note))
		}})
	}
	o.list.Rows = append(o.list.Rows, Row{Text: "другая", Render: func(sel bool, w int) string {
		return SelRow(sel, w, styleDim.Render(ui.Fit("другая папка...", 22)+"введи путь"))
	}})
	if o.list.Cursor >= len(o.list.Rows) {
		o.list.Cursor = 0
	}
}

func (o *onboard) fillFound() {
	o.list.Rows = nil
	for i := range o.found {
		i := i
		o.list.Rows = append(o.list.Rows, Row{Text: o.found[i].Name, Render: func(sel bool, w int) string {
			p := o.found[i]
			box := styleDim.Render("[ ]")
			if p.Checked {
				box = styleGood.Render("[" + symCheck + "]")
			}
			name := styleText.Render(ui.Fit(p.Name, 22))
			if sel {
				name = styleBold.Render(ui.Fit(p.Name, 22))
			}
			return SelRow(sel, w, box+" "+name+styleDim.Render(ui.Fit(p.Stack, 11)+ui.Fit(p.Age, 10)+ui.Clip(p.Dir, max(10, w-56))))
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

func (o *onboard) Update(msg tea.Msg, app *App) tea.Cmd {
	switch msg := msg.(type) {
	case onbRootsMsg:
		o.roots, o.busy = []RootChoice(msg), ""
		o.fillRoots()
		return nil
	case onbFoundMsg:
		o.found, o.busy, o.step = []FoundProject(msg), "", 1
		o.fillFound()
		return nil
	case onbDoneMsg:
		o.busy = ""
		if msg.err != nil {
			return app.Toast("bad", msg.err.Error())
		}
		switch o.step {
		case 1:
			o.saved, o.step = msg.text, 2
			return nil
		default:
			o.shellOK = msg.text
			return o.finish(app)
		}
	case tea.KeyPressMsg:
		k := msg.String()
		if o.busy != "" {
			return nil
		}
		if o.input.Focused() {
			if k == "enter" {
				path := strings.TrimSpace(o.input.Value())
				if path == "" {
					return app.Toast("warn", "введи путь к папке с проектами")
				}
				o.input.Blur()
				return o.scan(path)
			}
			var cmd tea.Cmd
			o.input, cmd = o.input.Update(msg)
			return cmd
		}
		switch o.step {
		case 0:
			if o.list.Key(k, 10) {
				return nil
			}
			if k == "enter" {
				if o.list.Cursor >= len(o.roots) {
					return o.input.Focus()
				}
				return o.scan(o.roots[o.list.Cursor].Path)
			}
		case 1:
			if o.list.Key(k, 10) {
				return nil
			}
			switch k {
			case "space", " ":
				if o.list.Cursor < len(o.found) {
					o.found[o.list.Cursor].Checked = !o.found[o.list.Cursor].Checked
					o.list.Down()
				}
			case "a", "ф":
				all := len(o.chosen()) == len(o.found)
				for i := range o.found {
					o.found[i].Checked = !all
				}
			case "enter":
				chosen := o.chosen()
				o.busy = "записываю конфиг"
				root := o.root
				return func() tea.Msg {
					text, err := o.opts.Save(root, chosen)
					return onbDoneMsg{text, err}
				}
			}
		case 2:
			switch k {
			case "y", "н", "enter":
				if o.opts.Shell == nil {
					return o.finish(app)
				}
				o.busy = "подключаю к оболочке"
				return func() tea.Msg {
					text, err := o.opts.Shell()
					return onbDoneMsg{text, err}
				}
			case "n", "т":
				return o.finish(app)
			}
		}
	}
	return nil
}

func (o *onboard) finish(app *App) tea.Cmd {
	text := o.saved
	if o.shellOK != "" {
		text += "; " + o.shellOK
	}
	if o.opts.Home != nil {
		return tea.Batch(app.Replace(o.opts.Home()), app.Toast("ok", text))
	}
	return app.Pop()
}

func (o *onboard) View(width, height int) string {
	var out []string
	var head []string
	switch o.step {
	case 0:
		head = []string{"",
			"  " + stylePaneT.Render("Привет. Где лежат твои проекты?"),
			"  " + styleDim.Render("tim заглянет в папку, найдёт репозитории и заведёт реестр: имя, стек, папка."),
			"",
			"  " + styleHead.Render("ПАПКИ С КОДОМ"),
		}
	case 1:
		n := len(o.chosen())
		head = []string{"",
			"  " + stylePaneT.Render("Нашёл "+plural(len(o.found), "репозиторий", "репозитория", "репозиториев")+" в "+o.root) +
				styleDim.Render("  сними галочку с того, что не нужно в реестре"),
			"",
			"  " + styleHead.Render("    "+ui.Fit("ИМЯ", 22)+ui.Fit("СТЕК", 11)+ui.Fit("МЕНЯЛСЯ", 10)+"ПАПКА") + styleDim.Render("   отмечено "+itoa(n)),
		}
	default:
		head = []string{"",
			"  " + stylePaneT.Render("Готово: "+o.saved),
			"",
			"  " + styleText.Render("Подключить tim к оболочке?"),
			"  " + styleDim.Render("Одна строка в файле настроек shell: tim go станет делать cd, появятся подсказки и дополнение."),
			"  " + styleDim.Render("Можно и потом: tim init"),
		}
	}
	if o.step == 0 && len(o.opts.Art) > 0 {
		head = withArt(head, o.opts.Art, width)
	}
	out = append(out, head...)
	if o.busy != "" {
		out = append(out, "", "  "+styleEmpty.Render(o.busy+"..."))
		return strings.Join(out, "\n")
	}
	if o.step < 2 {
		listH := max(3, height-len(out)-5)
		out = append(out, o.list.View(width, listH)...)
		if o.step == 0 && o.input.Focused() {
			out = append(out, "", "  "+o.input.View())
		}
		if o.step == 1 && len(o.found) == 0 {
			out = append(out, "  "+styleDim.Render("репозиториев не нашёл; enter запишет конфиг с пустым реестром, esc - другая папка"))
		}
	}
	for len(out) < height-2 {
		out = append(out, "")
	}
	switch o.step {
	case 0:
		out = append(out, "  "+styleDim.Render("Конфиг будет здесь: ~/.config/tim/config.toml. Его можно править руками, tim перечитывает сам."))
	case 1:
		out = append(out, "  "+styleDim.Render("Порты выдаются при запуске с 8000, чтобы проекты не толкались. Поменять: tim proj или правка конфига."))
	}
	return strings.Join(out, "\n")
}

// withArt: картинка справа от заголовка, если влезает.
func withArt(lines, art []string, width int) []string {
	if width < 90 {
		return lines
	}
	col := width - 4 - len([]rune(stripANSI(art[0])))
	out := make([]string, max(len(lines), len(art)))
	for i := range out {
		l := ""
		if i < len(lines) {
			l = lines[i]
		}
		if i < len(art) {
			l = ui.Fit(l, col) + art[i]
		}
		out[i] = l
	}
	return out
}

func stripANSI(s string) string {
	var b strings.Builder
	in := false
	for _, r := range s {
		switch {
		case r == 0x1b:
			in = true
		case in && (r == 'm'):
			in = false
		case !in:
			b.WriteRune(r)
		}
	}
	return b.String()
}
