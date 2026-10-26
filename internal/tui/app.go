package tui

import (
	"io"
	"reflect"
	"slices"
	"strings"
	"time"
	"unicode"

	tea "charm.land/bubbletea/v2"

	"github.com/TimurTurdyev/tim/internal/level"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// Экранный режим tim: один каркас у всех экранов - верхняя строка с крошками, тело, строка
// состояния, строка клавиш. Экраны лежат стопкой: esc возвращает на предыдущий, с корневого
// выходит со второго нажатия.

// animEvery: шаг общего тикера всех анимаций - кадр работы, моргание, идущие секунды.
const animEvery = 250 * time.Millisecond

// escWindow: сколько ждать второго esc на корневом экране.
const escWindow = 2 * time.Second

// animFrame: кадр общего тикера; экраны берут из него кадр работы через Spinner.
var animFrame int

// Key: подсказка клавиши для нижней строки и справки.
type Key struct{ K, D string }

// Screen: один экран. Тело рисует сам, остальное - каркас. Корневой экран (главный, мастер)
// отдаёт пустые крошки: верхней строки у него нет, её место занимает лок-ап.
type Screen interface {
	Crumbs() []string
	Status() string
	Keys() []Key
	Command() string // чем то же самое делается из терминала; пусто - не показывать
	Update(msg tea.Msg, app *App) tea.Cmd
	View(width, height int) string
}

// cursored: экран со строкой ввода, которой нужен настоящий курсор терминала. Без него курсор
// остаётся там, где закончилась последняя перерисовка, и подсветка строки курсора в iTerm ложится,
// например, на моргнувшего маскота.
type cursored interface {
	Cursor() *tea.Cursor
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

// helpKeys: в справке у экрана клавиш больше, чем в нижней строке.
type helpKeys interface {
	HelpKeys() []Key
}

// animated: экрану нужен общий тикер, пока он открыт (моргание маскота, бегунок в теле).
// Tick зовётся на каждый кадр.
type animated interface {
	Animating() bool
	Tick(frame int)
}

// Action: что сделать по выбору пользователя. Заполнено что-то одно.
type Action struct {
	Push Screen                 // открыть экран поверх
	Run  func() (string, error) // выполнить и показать результат
	Exit func()                 // закрыть tim и сделать это уже в терминале
	Dir  string                 // закрыть tim и перейти в папку (через шим)
	// Command: открыть строку ":" с этим текстом для правки - пример с аргументами, утилита.
	Command string
	Ask     *Ask // спросить строку внизу экрана
}

// Ask: строка ввода на месте строки состояния и что сделать с набранным.
type Ask struct {
	Placeholder, Hint string
	Done              func(text string) Action
}

// CommandEntry: подсказка для палитры ":".
type CommandEntry struct {
	Line, Desc string
	Name       string // как показать в палитре; пусто - сама строка
	Template   bool   // пример с выдуманными аргументами: выбор подставляет его для правки, а не выполняет
	Group      string // недавнее, подстановка (настоящие имена); пусто - команды
	// Confirm: перед запуском спросить; Yes окна зовётся до запуска строки - например запомнить согласие.
	Confirm *Dialog
}

// Paletter: у экрана своя группа в палитре - заготовки сервера, задачи проекта. Она стоит первой;
// title вида "на сервере prod · этот экран": до первой точки - заголовок, дальше - пояснение.
type Paletter interface {
	Palette() (title string, entries []CommandEntry)
}

// Options: что каркасу нужно от главного экрана.
type Options struct {
	Commands   func(prefix string) []CommandEntry // подсказки для ":"
	RunCommand func(line string) Action           // что делать с набранной командой
	CdFile     string                             // куда записать папку для шима, если выбрали "перейти"
}

// overlay: окно поверх экрана; compose кладёт его на приглушённые строки экрана.
type overlay interface {
	update(msg tea.KeyPressMsg, app *App) tea.Cmd
	compose(base []string, width, height int) []string
	keys() []Key
}

type animMsg struct{}

// ownedMsg: ответ команды, которую запустил экран id. Доходит только до него, даже если поверх открыт
// такой же экран; у закрытого экрана ответ пропадает, и его тикер останавливается.
type ownedMsg struct {
	id  int
	msg tea.Msg
}

// frame: экран в стопке и его номер для ответов.
type frame struct {
	s  Screen
	id int
}

// App: стопка экранов и общее состояние.
type App struct {
	opts    Options
	stack   []frame
	lastID  int
	width   int
	height  int
	overlay overlay
	say     sayState
	jobs    []job
	lastJob int
	lastErr string // полный текст последней ошибки - первой строкой палитры
	ticking bool
	escAt   time.Time
	Dir     string // куда перейти после выхода
	after   func()
	quit    bool
	askedBg bool // фон терминала уже спрошен
}

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
	app := &App{opts: opts, stack: []frame{{s: root, id: 1}}, lastID: 1, width: 100, height: 30}
	setStyles(ui.Light())
	// Строки "$ ..." работ в фоне не должны печататься поверх экрана.
	restore := ui.Divert(io.Discard)
	m, err := tea.NewProgram(app, tea.WithInput(tty), tea.WithOutput(tty)).Run()
	restore()
	if err != nil {
		return "", err
	}
	a := m.(*App)
	if a.after != nil {
		a.after()
	}
	return a.Dir, nil
}

// Spinner: текущий кадр работы - четвертинка клетки.
func Spinner() string { return spinFrames[animFrame%len(spinFrames)] }

func (a *App) cur() Screen { return a.top().s }

func (a *App) top() frame { return a.stack[len(a.stack)-1] }

// Push открывает экран поверх текущего.
func (a *App) Push(s Screen) tea.Cmd {
	a.lastID++
	f := frame{s: s, id: a.lastID}
	a.stack = append(a.stack, f)
	a.overlay = nil
	return a.init(f)
}

func (a *App) init(f frame) tea.Cmd {
	if in, ok := f.s.(Initer); ok {
		return own(f.id, in.Init())
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
	case act.Command != "":
		a.overlay = newCmdline(a, act.Command)
	case act.Ask != nil:
		done := act.Ask.Done
		return a.Ask(act.Ask.Placeholder, act.Ask.Hint, func(text string) tea.Cmd { return a.Do(done(text)) })
	}
	return nil
}

// Confirm показывает окно подтверждения; yes вызывается по согласию.
func (a *App) Confirm(d Dialog) tea.Cmd {
	a.overlay = &confirmOverlay{d: d}
	return nil
}

func (a *App) Init() tea.Cmd { return tea.Batch(a.init(a.top()), a.wake()) }

func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	cmd := a.update(msg)
	return a, tea.Batch(cmd, a.wake())
}

func (a *App) update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.width, a.height = msg.Width, msg.Height
		// Размер приходит сразу после старта - тогда же один раз спрашивается фон терминала.
		if !a.askedBg {
			a.askedBg = true
			return tea.RequestBackgroundColor
		}
		return nil
	case tea.BackgroundColorMsg:
		// Строки экрана уже покрашены через ui прежней палитрой - экран собирается заново.
		if useBackground(msg) {
			return a.init(a.top())
		}
		return nil
	case sayExpireMsg:
		if msg.id == a.say.id {
			a.say = sayState{}
		}
		return nil
	case animMsg:
		a.ticking = false
		animFrame++
		if an, ok := a.cur().(animated); ok {
			an.Tick(animFrame)
		}
		return nil
	case tea.KeyPressMsg:
		return a.key(msg)
	case ownedMsg:
		// Ответ фоновой загрузки должен дойти до своего экрана, даже если поверх уже открыт другой.
		return a.deliver(msg.id, msg.msg)
	case jobDoneMsg:
		owner, cmd := a.finish(msg)
		return tea.Batch(cmd, a.deliver(owner, msg.done))
	}
	var cmds []tea.Cmd
	for _, f := range a.stack {
		if c := f.s.Update(msg, a); c != nil {
			cmds = append(cmds, own(f.id, c))
		}
	}
	return tea.Batch(cmds...)
}

// deliver отдаёт сообщение экрану id, если он ещё в стопке.
func (a *App) deliver(id int, msg tea.Msg) tea.Cmd {
	for _, f := range a.stack {
		if f.id == id {
			return own(f.id, f.s.Update(msg, a))
		}
	}
	return nil
}

// key: порядок разбора клавиши - наложение, ctrl+c, ? : q (если не набор), esc, экран.
// Строка состояния клавиш не перехватывает: ошибка и предупреждение гаснут, а клавиша идёт дальше.
func (a *App) key(msg tea.KeyPressMsg) tea.Cmd {
	if a.say.text != "" && a.say.lvl != level.OK && !a.say.plain {
		a.say = sayState{id: a.say.id + 1}
	}
	if msg.String() == "ctrl+c" {
		a.quit = true
		return tea.Quit
	}
	if _, ok := a.overlay.(*helpOverlay); ok {
		// Справка закрывается любой клавишей, и эта клавиша делает своё, кроме esc и самого ?.
		a.overlay = nil
		if k := msg.String(); k == "esc" || k == "?" {
			return nil
		}
	}
	if a.overlay != nil {
		// Окно открыл верхний экран: ответ его действия - ему.
		return own(a.top().id, a.overlay.update(msg, a))
	}
	msg = a.translate(msg)
	typing := a.typing()
	switch msg.String() {
	case "esc":
		return a.escape()
	case "?":
		if !typing {
			ks := a.screenKeys(a.cur())
			if h, ok := a.cur().(helpKeys); ok {
				ks = h.HelpKeys()
			}
			a.overlay = &helpOverlay{title: screenTitle(a.cur()), ks: slices.DeleteFunc(ks, func(k Key) bool { return quietKey(k.K) })}
			return nil
		}
	case ":":
		if !typing {
			a.overlay = newCmdline(a, "")
			return nil
		}
	case "q":
		if !typing {
			a.quit = true
			return tea.Quit
		}
	}
	if a.busy() {
		return nil
	}
	f := a.top()
	return own(f.id, f.s.Update(msg, a))
}

func (a *App) typing() bool {
	t, ok := a.cur().(Typer)
	return ok && t.Typing()
}

// translate: русская буква становится латинской с той же клавиши. Только там, где нет живой
// строки ввода: на главном и в наборе русская буква - это текст.
func (a *App) translate(msg tea.KeyPressMsg) tea.KeyPressMsg {
	if c, ok := a.cur().(cursored); ok && c.Cursor() != nil || a.typing() {
		return msg
	}
	r := []rune(msg.Text)
	if len(r) != 1 {
		return msg
	}
	lat, ok := ruLayout[unicode.ToLower(r[0])]
	if !ok {
		return msg
	}
	if unicode.IsUpper(r[0]) {
		lat = unicode.ToUpper(lat)
	}
	return tea.KeyPressMsg{Code: unicode.ToLower(lat), Text: string(lat), Mod: msg.Mod}
}

// escape: сначала экран (закрыть фильтр, очистить ввод), потом назад; на корневом первый esc
// только подсказывает, второй выходит - чтобы, листая назад, не вылететь из tim.
func (a *App) escape() tea.Cmd {
	if e, ok := a.cur().(Escaper); ok && e.Escape() {
		return nil
	}
	if len(a.stack) > 1 {
		a.stack = a.stack[:len(a.stack)-1]
		return nil
	}
	if time.Since(a.escAt) < escWindow {
		a.quit = true
		return tea.Quit
	}
	a.escAt = time.Now()
	return a.hint("esc ещё раз - выход "+symSep+" q - выход", escWindow)
}

// wake запускает общий тикер, если что-то движется: работа, кадр в статусе, экран.
func (a *App) wake() tea.Cmd {
	if a.ticking || a.quit || !a.animating() {
		return nil
	}
	a.ticking = true
	return tea.Tick(animEvery, func(time.Time) tea.Msg { return animMsg{} })
}

func (a *App) animating() bool {
	if len(a.jobs) > 0 {
		return true
	}
	s := a.cur()
	if strings.HasPrefix(s.Status(), spinFrames[0]) {
		return true
	}
	an, ok := s.(animated)
	return ok && an.Animating()
}

// own помечает ответы команды экрана его номером. Сообщения самого bubbletea (выход, запуск
// программы, пачки) идут как есть - их разбирает рантайм, а пачка помечается поштучно.
func own(id int, c tea.Cmd) tea.Cmd {
	if c == nil {
		return nil
	}
	return func() tea.Msg {
		switch m := c().(type) {
		case nil, ownedMsg:
			return m
		case tea.BatchMsg:
			out := make(tea.BatchMsg, len(m))
			for i, cc := range m {
				out[i] = own(id, cc)
			}
			return out
		default:
			if runtimeMsg(m) || appMsg(m) {
				return m
			}
			return ownedMsg{id: id, msg: m}
		}
	}
}

// appMsg: сообщение самого каркаса - его разбирает App, а не экран.
func appMsg(m tea.Msg) bool {
	switch m.(type) {
	case jobDoneMsg, sayExpireMsg, animMsg:
		return true
	}
	return false
}

func runtimeMsg(m tea.Msg) bool {
	t := reflect.TypeOf(m)
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return strings.HasPrefix(t.PkgPath(), "charm.land/bubbletea/")
}
