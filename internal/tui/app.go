package tui

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// Экранный режим tim: один каркас у всех экранов - верхняя строка с крошками, тело, строка клавиш.
// Экраны лежат стопкой: esc возвращает на предыдущий, с первого - выходит.

// Key: подсказка клавиши для нижней строки и справки.
type Key struct{ K, D string }

// Screen: один экран. Тело рисует сам, остальное - каркас.
type Screen interface {
	Crumbs() []string
	Status() string
	Keys() []Key
	Command() string // чем то же самое делается из терминала; пусто - не показывать
	Update(msg tea.Msg, app *App) tea.Cmd
	View(width, height int) string
}

// Typer: экран, который прямо сейчас принимает набираемый текст (поиск). Тогда буквы и знаки
// идут ему, а не каркасу; esc всё равно остаётся за каркасом.
type Typer interface {
	Typing() bool
}

// Escaper: экран, который сначала сам обрабатывает esc (закрыть фильтр), и только потом - назад.
type Escaper interface {
	Escape() bool
}

// Initer: экрану нужно что-то запустить при открытии (замер, запрос).
type Initer interface {
	Init() tea.Cmd
}

// Action: что сделать по выбору пользователя. Заполнено что-то одно.
type Action struct {
	Push Screen                 // открыть экран поверх
	Run  func() (string, error) // выполнить и показать результат
	Exit func()                 // закрыть tim и сделать это уже в терминале
	Dir  string                 // закрыть tim и перейти в папку (через шим)
}

// CommandEntry: подсказка для командной строки ":".
type CommandEntry struct {
	Line, Desc string
}

// Options: что каркасу нужно от cmd.
type Options struct {
	Commands   func(prefix string) []CommandEntry // подсказки для ":"
	RunCommand func(line string) Action           // что делать с набранной командой
	CdFile     string                             // куда записать папку для шима, если выбрали "перейти"
}

type overlay interface {
	update(msg tea.KeyPressMsg, app *App) tea.Cmd
	view(width, height int) string
	keys() []Key
}

type toastMsg struct{ id int }

type toastState struct {
	text string
	cls  lipgloss.Style
	id   int
}

// App: стопка экранов и общее состояние.
type App struct {
	opts    Options
	stack   []Screen
	width   int
	height  int
	overlay overlay
	toast   toastState
	Dir     string // куда перейти после выхода
	after   func()
	quit    bool
}

var (
	styleTop     = lipgloss.NewStyle().Background(colBarBg)
	styleBrand   = lipgloss.NewStyle().Background(colAccent).Foreground(colOnAcc).Bold(true)
	styleCrumb   = lipgloss.NewStyle().Foreground(colText).Background(colBarBg)
	styleStatus  = lipgloss.NewStyle().Foreground(colDim).Background(colBarBg)
	styleKeyBar  = lipgloss.NewStyle().Background(colBarBg)
	styleKeyCap  = lipgloss.NewStyle().Background(colKeyBg).Foreground(colInfo)
	styleKeyDesc = lipgloss.NewStyle().Foreground(colDim).Background(colBarBg)
	styleToastOK = lipgloss.NewStyle().Foreground(colOK).Bold(true)
	styleToastWn = lipgloss.NewStyle().Foreground(colWarn).Bold(true)
	styleToastBd = lipgloss.NewStyle().Foreground(colBad).Bold(true)
)

// Run открывает экран на весь терминал и держит, пока пользователь не выйдет.
// Возвращает папку, куда просили перейти (через TIM_CD_FILE её подхватит шим).
func Run(root Screen, opts Options) (dir string, err error) {
	if !Available() {
		return "", ErrNoTTY
	}
	tty, err := openTTY()
	if err != nil {
		return "", ErrNoTTY
	}
	defer func() { _ = tty.Close() }()
	app := &App{opts: opts, stack: []Screen{root}, width: 100, height: 30}
	m, err := tea.NewProgram(app, tea.WithInput(tty), tea.WithOutput(tty)).Run()
	if err != nil {
		return "", err
	}
	a := m.(*App)
	if a.after != nil {
		a.after()
	}
	return a.Dir, nil
}

func (a *App) cur() Screen { return a.stack[len(a.stack)-1] }

// Push открывает экран поверх текущего.
func (a *App) Push(s Screen) tea.Cmd {
	a.stack = append(a.stack, s)
	a.overlay = nil
	if in, ok := s.(Initer); ok {
		return in.Init()
	}
	return nil
}

// Replace меняет текущий экран на другой: мастер первого запуска уступает место главному.
func (a *App) Replace(s Screen) tea.Cmd {
	a.stack = a.stack[:len(a.stack)-1]
	return a.Push(s)
}

// Pop закрывает текущий экран; с последнего - выход.
func (a *App) Pop() tea.Cmd {
	if len(a.stack) > 1 {
		a.stack = a.stack[:len(a.stack)-1]
		return nil
	}
	a.quit = true
	return tea.Quit
}

// Do выполняет действие экрана.
func (a *App) Do(act Action) tea.Cmd {
	switch {
	case act.Push != nil:
		return a.Push(act.Push)
	case act.Run != nil:
		return a.Push(newResult(act.Run))
	case act.Dir != "":
		a.Dir = act.Dir
		a.quit = true
		return tea.Quit
	case act.Exit != nil:
		a.after = act.Exit
		a.quit = true
		return tea.Quit
	}
	return nil
}

// Toast показывает строку внизу на несколько секунд.
func (a *App) Toast(kind, text string) tea.Cmd {
	st := styleToastOK
	mark := symCheck
	switch kind {
	case "warn":
		st, mark = styleToastWn, symWarn
	case "bad":
		st, mark = styleToastBd, symCross
	}
	a.toast = toastState{text: mark + " " + text, cls: st, id: a.toast.id + 1}
	id := a.toast.id
	return tea.Tick(4*time.Second, func(time.Time) tea.Msg { return toastMsg{id} })
}

// Confirm показывает окно подтверждения; yes вызывается по согласию.
func (a *App) Confirm(d Dialog) tea.Cmd {
	a.overlay = &confirmOverlay{d: d}
	return nil
}

func (a *App) Init() tea.Cmd {
	if in, ok := a.cur().(Initer); ok {
		return in.Init()
	}
	return nil
}

func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.width, a.height = msg.Width, msg.Height
		return a, nil
	case toastMsg:
		if msg.id == a.toast.id {
			a.toast.text = ""
		}
		return a, nil
	case tea.KeyPressMsg:
		if a.overlay != nil {
			return a, a.overlay.update(msg, a)
		}
		typing := false
		if t, ok := a.cur().(Typer); ok {
			typing = t.Typing()
		}
		switch msg.String() {
		case "ctrl+c":
			a.quit = true
			return a, tea.Quit
		case "esc":
			if e, ok := a.cur().(Escaper); ok && e.Escape() {
				return a, nil
			}
			return a, a.Pop()
		case "?":
			if !typing {
				a.overlay = &helpOverlay{ks: a.cur().Keys()}
				return a, nil
			}
		case ":":
			if !typing {
				a.overlay = newCmdline(a)
				return a, nil
			}
		case "q", "й":
			if !typing {
				a.quit = true
				return a, tea.Quit
			}
		}
	}
	if _, ok := msg.(tea.KeyPressMsg); ok {
		return a, a.cur().Update(msg, a)
	}
	// Ответ фоновой загрузки должен дойти до своего экрана, даже если поверх уже открыт другой.
	var cmds []tea.Cmd
	for _, s := range a.stack {
		if c := s.Update(msg, a); c != nil {
			cmds = append(cmds, c)
		}
	}
	return a, tea.Batch(cmds...)
}

func (a *App) View() tea.View {
	if a.quit {
		return tea.NewView("")
	}
	w, h := a.width, a.height
	s := a.cur()
	top := a.topBar(s)
	bottom := a.keyBar(s)
	bodyH := max(1, h-2)
	var body string
	if a.overlay != nil {
		body = a.overlay.view(w, bodyH)
	} else {
		body = s.View(w, bodyH)
	}
	lines := strings.Split(body, "\n")
	if len(lines) > bodyH {
		lines = lines[:bodyH]
	}
	for len(lines) < bodyH {
		lines = append(lines, "")
	}
	if a.toast.text != "" {
		lines[bodyH-1] = " " + a.toast.cls.Render(a.toast.text)
	}
	v := tea.NewView(top + "\n" + strings.Join(lines, "\n") + "\n" + bottom)
	v.AltScreen = true
	return v
}

func (a *App) topBar(s Screen) string {
	left := styleBrand.Render(" tim ")
	crumbs := ""
	for _, c := range s.Crumbs() {
		crumbs += " " + symPointer + " " + c
	}
	left += styleCrumb.Render(crumbs)
	right := styleStatus.Render(s.Status() + " ")
	gap := a.width - lipgloss.Width(left) - lipgloss.Width(right)
	return styleTop.Render(left + strings.Repeat(" ", max(1, gap)) + right)
}

func (a *App) keyBar(s Screen) string {
	keys := s.Keys()
	if a.overlay != nil {
		keys = a.overlay.keys()
	}
	var parts []string
	for _, k := range keys {
		parts = append(parts, styleKeyCap.Render(" "+k.K+" ")+styleKeyDesc.Render(" "+k.D))
	}
	left := " " + strings.Join(parts, styleKeyDesc.Render("  "))
	right := ""
	if c := s.Command(); c != "" && a.overlay == nil {
		right = styleKeyDesc.Render(c + " ")
	}
	gap := a.width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		right = ""
		gap = 1
	}
	return styleKeyBar.Render(left + strings.Repeat(" ", gap) + right)
}

// globalKeys: то, что одинаково на всех экранах - для справки.
var globalKeys = []Key{{"esc", "назад, с первого - выход"}, {"?", "справка"}, {":", "любая команда tim"}, {"q", "выход"}}
