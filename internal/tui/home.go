package tui

import (
	"math/rand/v2"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/TimurTurdyev/tim/internal/brand"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// Главный экран: лок-ап, строка поиска, список из групп ВНИМАНИЕ, НЕДАВНЕЕ и МАШИНА, разделы справа.
// Верхней строки нет - её место занимает лок-ап.

// Группы списка главного экрана.
const (
	KindAttention = "внимание"
	KindRecent    = "недавнее"
	KindMachine   = "машина"
)

// Колонки главного экрана: левая - список, справа от неё разделы; у узкого окна всё уже.
const (
	homeLeftW      = 55 // левая колонка на обычном окне
	homeLeftNarrow = 43 // и на 80 колонках
	homeNameW      = 34 // имя недавнего, пометка "правка в :" внутри него
	homeMinRecent  = 5  // меньше строк недавнего - блока нет
	homeAir        = 3  // воздух под списком до МАШИНЫ
	homeMachineW   = 55 // значение строки машины, по которому выравнивается "куда ведёт"
)

// Hit: то, что находится поиском и лежит в группах главного - проект, команда, беда, строка машины.
type Hit struct {
	Kind  string // группа: внимание, недавнее, машина, команды, проекты...
	Title string
	Hint  string // серым справа; у беды и строки машины - куда ведёт
	Note  string // цветное справа, например лампа и порт; у строки машины - значение
	Edit  bool   // меняющая команда: enter подставит её в ":" для правки
	Open  func() Action
}

// Section: раздел с буквой перехода; Badge - число бед раздела цветом лампы.
type Section struct {
	Key, Title, Hint, Badge string
	Open                    func() Action
}

// HomeData: быстрая часть главного - лок-ап, недавнее, разделы.
type HomeData struct {
	Header   [][]string
	Recent   []Hit
	Sections []Section
}

// HomeMachine: медленная часть - беды, строки машины, настроение и бейджи разделов.
type HomeMachine struct {
	Mood      brand.Mood
	Attention []Hit
	Machine   []Hit
	Badges    map[string]string // буква раздела -> бейдж
}

// HomeOpts: источники главного экрана.
type HomeOpts struct {
	Load    func() HomeData
	Machine func() HomeMachine
	Search  func(q string) []Hit
}

type home struct {
	opts     HomeOpts
	data     HomeData
	mach     HomeMachine
	input    textinput.Model
	list     List
	hits     []Hit
	ready    bool // быстрая часть пришла
	machOK   bool // и медленная
	started  time.Time
	blink    bool
	shutAt   int // кадр общего тикера, на котором глаза закроются
	inputRow int
	hidden   map[int]bool // строки списка, которые не влезли: курсор их пропускает
}

type (
	homeDataMsg    HomeData
	homeMachineMsg HomeMachine
)

// NewHome создаёт главный экран.
func NewHome(opts HomeOpts) Screen {
	in := textinput.New()
	in.Prompt = symPointer + " "
	// Пробел первым: курсор стоит на нём, а не закрывает первую букву подсказки.
	in.Placeholder = " проект, команда, домен, порт - начни печатать"
	in.SetWidth(56)
	st := in.Styles()
	st.Focused.Prompt = stylePrompt
	st.Focused.Placeholder = styleHint
	st.Cursor.Color = colAccent
	in.SetStyles(st)
	in.SetVirtualCursor(false)
	in.Focus()
	return &home{opts: opts, input: in}
}

// Cursor: настоящий курсор в строке ввода; её строку знает View.
func (h *home) Cursor() *tea.Cursor {
	c := h.input.Cursor()
	if c == nil {
		return nil
	}
	c.X += 3
	c.Y += h.inputRow
	return c
}

func (h *home) Init() tea.Cmd {
	h.started = time.Now()
	return tea.Batch(textinput.Blink, h.reload())
}

func (h *home) reload() tea.Cmd {
	h.machOK = false
	return tea.Batch(
		func() tea.Msg { return homeDataMsg(h.opts.Load()) },
		func() tea.Msg { return homeMachineMsg(h.opts.Machine()) },
	)
}

// Animating: маскот моргает, пока главный открыт; секунды сбора идут, пока он не закончен.
func (h *home) Animating() bool { return true }

// Tick: глаза закрыты один кадр, следующее моргание через 3-7 секунд.
func (h *home) Tick(frame int) {
	h.blink = frame == h.shutAt
	if frame > h.shutAt {
		h.shutAt = frame + 12 + rand.IntN(16)
	}
}

// Escape: esc сначала стирает набранное.
func (h *home) Escape() bool {
	if h.input.Value() == "" {
		return false
	}
	h.input.SetValue("")
	h.refill()
	return true
}

func (h *home) Crumbs() []string { return nil }
func (h *home) Status() string   { return "" }
func (h *home) Command() string  { return "" }
func (h *home) Typing() bool     { return h.input.Value() != "" }

func (h *home) Keys() []Key {
	if h.Typing() {
		return []Key{{"enter", "открыть"}, {"tab", "дополнить"}, {"esc", "очистить"}}
	}
	return []Key{{"enter", "открыть"}}
}

// refill: в поиске - находки, иначе группы ВНИМАНИЕ, НЕДАВНЕЕ и МАШИНА; курсор - на недавнем.
func (h *home) refill() {
	if q := strings.TrimSpace(h.input.Value()); q != "" && h.opts.Search != nil {
		h.hits = h.opts.Search(q)
	} else {
		h.hits = nil
		h.hits = append(h.hits, h.mach.Attention...)
		h.hits = append(h.hits, h.data.Recent...)
		h.hits = append(h.hits, h.mach.Machine...)
	}
	h.list.Rows = nil
	lastKind := ""
	for i := range h.hits {
		e := &h.hits[i]
		if e.Kind != lastKind {
			lastKind = e.Kind
			h.list.Rows = append(h.list.Rows, Row{Header: true, Text: e.Kind})
		}
		h.list.Rows = append(h.list.Rows, Row{Text: e.Title})
	}
	h.list.Cursor = 0
	for i, r := range h.list.Rows {
		if r.Header && r.Text == KindRecent {
			h.list.Cursor = i
			break
		}
	}
	h.list.Skip(1)
}

// hitAt: находка под строкой списка i; заголовок - nil.
func (h *home) hitAt(i int) *Hit {
	n := 0
	for j, r := range h.list.Rows {
		if r.Header {
			continue
		}
		if j == i {
			return &h.hits[n]
		}
		n++
	}
	return nil
}

func (h *home) Update(msg tea.Msg, app *App) tea.Cmd {
	switch msg := msg.(type) {
	case homeDataMsg:
		h.data, h.ready = HomeData(msg), true
		h.refill()
		return nil
	case homeMachineMsg:
		h.mach, h.machOK = HomeMachine(msg), true
		h.refill()
		return nil
	case tea.KeyPressMsg:
		switch msg.String() {
		case "up", "ctrl+p":
			h.move(-1)
			return nil
		case "down", "ctrl+n":
			h.move(1)
			return nil
		case "pgup", "pgdown", "home", "end":
			h.list.Key(msg.String(), 10)
			return nil
		case "tab":
			if e := h.hitAt(h.list.Cursor); e != nil && h.Typing() {
				h.input.SetValue(e.Title)
				h.input.CursorEnd()
				h.refill()
			}
			return nil
		case "enter":
			if e := h.hitAt(h.list.Cursor); e != nil && e.Open != nil {
				return app.Do(e.Open())
			}
			return nil
		case "ctrl+r":
			return h.reload()
		}
		// Буквы идут в поиск, а не открывают раздел сразу: иначе не найти ssh или проект на s.
		// Буква раздела в поиске ставит раздел первым - он открывается по enter.
	}
	var cmd tea.Cmd
	before := h.input.Value()
	h.input, cmd = h.input.Update(msg)
	if h.input.Value() != before {
		h.refill()
	}
	return cmd
}

// move: курсор на соседнюю видимую строку; не влезшие строки групп пропускаются.
func (h *home) move(dir int) {
	from := h.list.Cursor
	for {
		before := h.list.Cursor
		if dir < 0 {
			h.list.Up()
		} else {
			h.list.Down()
		}
		switch {
		case h.list.Cursor == before:
			h.list.Cursor = from
			return
		case !h.hidden[h.list.Cursor]:
			return
		}
	}
}

// look: настроение машины, поверх него - исход последнего действия сессии; моргание.
func (h *home) look() brand.Look {
	l := brand.Look{Mood: sessionMood(h.mach.Mood), Light: ui.Light()}
	if h.blink {
		l.Mood |= brand.Blink
	}
	return l
}

func (h *home) View(width, height int) string {
	var out []string
	if height >= 30 {
		out = append(out, "")
	}
	head := ui.Header(h.data.Header, ui.HeaderOpts{Look: h.look(), Indent: 2, Width: width,
		Sprite: width >= 70 && !dimmed, Keep: true})
	out = append(out, head...)
	out = append(out, "")
	h.inputRow = len(out)
	out = append(out, "   "+h.input.View(), "")
	if h.Typing() {
		return strings.Join(append(out, h.searchView(width, height-len(out))...), "\n")
	}
	h.hidden = map[int]bool{}
	leftW := homeLeftW
	if width < 110 {
		leftW = homeLeftNarrow
	}
	mach := h.machineView(width, height < 30 || width < 110)
	room := height - len(out) - len(mach) - 1
	if height >= 30 {
		room -= homeAir
	}
	left := h.groupsView(leftW, room)
	right := h.sectionsView(width-leftW, width >= 110)
	n := max(len(left), len(right))
	for i := range n {
		l, r := "", ""
		if i < len(left) {
			l = left[i]
		}
		if i < len(right) {
			r = right[i]
		}
		out = append(out, strings.TrimRight(ui.Fit(l, leftW)+r, " "))
	}
	out = append(append(out, ""), mach...)
	return strings.Join(out, "\n")
}
