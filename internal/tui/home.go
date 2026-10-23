package tui

import (
	"math/rand/v2"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/TimurTurdyev/tim/internal/ui"
)

// Главный экран: строка поиска, недавнее и разделы, плитки состояния, одно предупреждение.

// recentW: ширина колонки недавнего на главном экране.
const recentW = 52

// Hit: то, что находится поиском - проект, команда, сервер, домен, недавнее.
type Hit struct {
	Kind  string // заголовок группы: Команды, Проекты, Серверы...
	Title string
	Hint  string // серым справа
	Note  string // цветное справа, например "занято 72%" или "работает :8013"
	Open  func() Action
}

// Section: раздел с буквой перехода.
type Section struct {
	Key, Title, Hint string
	Open             func() Action
}

// HomeData: что показывает главный экран; всё считается в разделе home и обновляется по r.
type HomeData struct {
	Status   string
	Recent   []Hit
	Sections []Section
	Machine  []string // карточка машины: строки уже с цветом
	Warning  string
	WarnOpen func() Action
	// Art: маскот уже с цветом, крупно - для широкого окна, blink - кадр моргания; зовётся при
	// отрисовке, чтобы настроение успевало за событиями (вернулся после удачной команды - рад).
	// nil - без картинки.
	Art func(large, blink bool) []string
}

// HomeOpts: источники главного экрана.
type HomeOpts struct {
	Load   func() HomeData
	Search func(q string) []Hit
}

type home struct {
	opts   HomeOpts
	data   HomeData
	input  textinput.Model
	list   List
	hits   []Hit
	ready  bool
	blink  bool
	shutAt int // кадр общего тикера, на котором глаза закроются
}

type homeDataMsg HomeData

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

// Cursor: настоящий курсор в строке ввода - её место в View известно: третья строка, отступ в два.
func (h *home) Cursor() *tea.Cursor {
	c := h.input.Cursor()
	if c == nil {
		return nil
	}
	c.X += 2
	c.Y += 2
	return c
}

func (h *home) Init() tea.Cmd {
	return tea.Batch(textinput.Blink, func() tea.Msg { return homeDataMsg(h.opts.Load()) })
}

// Animating: маскот моргает, пока главный открыт.
func (h *home) Animating() bool { return h.data.Art != nil }

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
func (h *home) Status() string   { return h.data.Status }
func (h *home) Command() string  { return "" }
func (h *home) Typing() bool     { return h.input.Value() != "" }

func (h *home) Keys() []Key {
	if h.Typing() {
		return []Key{{"enter", "открыть"}, {"esc", "очистить"}}
	}
	return []Key{{"enter", "открыть"}}
}

func (h *home) refill() {
	h.hits = nil
	if q := strings.TrimSpace(h.input.Value()); q != "" && h.opts.Search != nil {
		h.hits = h.opts.Search(q)
	} else {
		h.hits = h.data.Recent
	}
	h.list.Rows = nil
	lastKind := ""
	// Колонка имени - по самому длинному имени, но подсказке справа остаётся место.
	titleW := 0
	for _, e := range h.hits {
		titleW = max(titleW, lipgloss.Width(e.Title)+2)
	}
	for _, e := range h.hits {
		e := e
		kind := e.Kind
		if e.Kind != lastKind {
			lastKind = e.Kind
			k := kind
			h.list.Rows = append(h.list.Rows, Row{Header: true, Render: func(sel bool, w int) string {
				return "  " + styleHead.Render(strings.ToUpper(k))
			}})
		}
		h.list.Rows = append(h.list.Rows, Row{Text: e.Title, Render: func(sel bool, w int) string {
			tw := min(titleW, 34, max(12, w-3-24))
			rest := w - 3 - tw - lipgloss.Width(e.Note)
			body := ui.Fit(styleBold.Render(e.Title), tw) + ui.Fit(styleDim.Render(e.Hint), max(8, rest-1)) + e.Note
			return SelRow(sel, w, body)
		}})
	}
	h.list.Home()
}

func (h *home) current() *Hit {
	n := 0
	for i, r := range h.list.Rows {
		if r.Header {
			continue
		}
		if i == h.list.Cursor {
			return &h.hits[n]
		}
		n++
	}
	return nil
}

func (h *home) Update(msg tea.Msg, app *App) tea.Cmd {
	switch msg := msg.(type) {
	case homeDataMsg:
		h.data = HomeData(msg)
		h.ready = true
		h.refill()
		return nil
	case tea.KeyPressMsg:
		k := msg.String()
		switch k {
		case "up", "ctrl+p":
			h.list.Up()
			return nil
		case "down", "ctrl+n", "tab":
			h.list.Down()
			return nil
		case "enter":
			if e := h.current(); e != nil && e.Open != nil {
				return app.Do(e.Open())
			}
			if h.input.Value() == "" && h.data.WarnOpen != nil && h.data.Warning != "" {
				return app.Do(h.data.WarnOpen())
			}
			return nil
		case "ctrl+r":
			return func() tea.Msg { return homeDataMsg(h.opts.Load()) }
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

func (h *home) View(width, height int) string {
	var out []string
	head := []string{
		"",
		"  " + styleTitle.Render("Что сделать?"),
		"  " + h.input.View(),
		"",
	}
	typing := h.Typing()
	var secs []string
	secW := 0
	for _, sc := range h.data.Sections {
		line := styleKeyCap.Render(" "+sc.Key+" ") + " " + ui.Fit(styleText.Render(sc.Title), 13) + styleDim.Render(sc.Hint)
		secs = append(secs, line)
		secW = max(secW, ui.Width(line))
	}
	// Недавнее и разделы рядом - только когда разделы влезают целиком, иначе друг под другом.
	wide := len(secs) > 0 && width >= recentW+secW+2
	need := max(recentW, secW+2)
	if wide {
		need = recentW + secW + 2
	}
	// Картинка справа сверху - только если ничего не срезает. Всё, что рядом с ней, собирается сразу
	// шириной cw: обрезка готовой строки ставила "~" на хвост из пробелов.
	var art []string
	if h.data.Art != nil && width >= 90 {
		for _, large := range []bool{true, false} {
			if a := h.data.Art(large, h.blink); len(a) > 0 && need+ui.Width(a[0])+2 <= width && (!large || width >= 140) {
				art = a
				break
			}
		}
	}
	cw := width
	if len(art) > 0 {
		cw = width - ui.Width(art[0]) - 2
	}
	var card []string
	if len(h.data.Machine) > 0 {
		card = append(card, "", "  "+styleHead.Render("МАШИНА"))
		for _, r := range h.data.Machine {
			card = append(card, "  "+ui.Clip(r, cw-2))
		}
	}
	out = append(out, head...)
	switch {
	case !typing && wide:
		// Недавнее слева, разделы справа; заголовок группы у недавнего уже есть в списке. Строк недавнего -
		// сколько влезает над карточкой, но не меньше, чем разделов рядом.
		avail := height - len(head) - len(card) - 5
		left := h.list.View(recentW, min(len(h.list.Rows), max(len(secs)+1, avail)))
		if len(left) == 0 {
			left = []string{"  " + styleHead.Render("НЕДАВНЕЕ"), "  " + styleHint.Render("проекты и команды, которые открывал, появятся здесь")}
		}
		right := append([]string{styleHead.Render("РАЗДЕЛЫ")}, secs...)
		out = append(out, Beside(left, recentW, right, cw)...)
	case !typing:
		// Друг под другом экран выше: недавнего столько, чтобы плитки и предупреждение остались видны.
		fit := height - len(head) - (len(secs) + 2) - len(card) - 4
		out = append(out, h.list.View(cw, min(max(3, fit), len(h.list.Rows)))...)
		out = append(out, "", "  "+styleHead.Render("РАЗДЕЛЫ"))
		for _, sc := range secs {
			out = append(out, "  "+sc)
		}
	default:
		out = append(out, h.list.View(cw, max(3, height-len(head)-2))...)
	}
	if !typing {
		out = append(out, card...)
		if h.data.Warning != "" {
			out = append(out, "")
			warn, hint := styleAmber.Render(symWarn+" "+h.data.Warning), styleDim.Render("   enter - разобраться")
			if ui.Width(warn+hint)+2 > width {
				hint = ""
			}
			out = append(out, "  "+warn+hint)
		}
	}
	if !h.ready {
		out = append(out, "  "+styleEmpty.Render("собираю сводку..."))
	}
	for i, a := range art {
		if i >= len(out) {
			out = append(out, "")
		}
		out[i] = ui.Fit(out[i], cw) + a
	}
	return strings.Join(out, "\n")
}
