package tui

import (
	"math/rand/v2"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/TimurTurdyev/tim/internal/ui"
)

// Главный экран: строка поиска, недавнее и разделы, плитки состояния, одно предупреждение.

// blinkShut: сколько глаза маскота закрыты при моргании.
const blinkShut = 150 * time.Millisecond

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

// TileData: плитка состояния; строки уже с цветом.
type TileData struct {
	Title string
	Rows  []string
	Width int
}

// HomeData: что показывает главный экран; всё считается в разделе home и обновляется по r.
type HomeData struct {
	Status   string
	Recent   []Hit
	Sections []Section
	Tiles    []TileData
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
	opts  HomeOpts
	data  HomeData
	input textinput.Model
	list  List
	hits  []Hit
	ready bool
	blink bool
	gen   int // поколение таймера моргания: повторный Init не запускает вторую цепочку
}

type homeDataMsg HomeData

// blinkMsg: глаза маскота закрыть или открыть.
type blinkMsg struct {
	gen  int
	shut bool
}

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
	in.Focus()
	return &home{opts: opts, input: in}
}

func (h *home) Init() tea.Cmd {
	h.gen++
	return tea.Batch(textinput.Blink, func() tea.Msg { return homeDataMsg(h.opts.Load()) }, h.blinkAfter(false))
}

// blinkAfter: закрытые глаза открыть через миг, открытые - закрыть через 3-7 секунд.
func (h *home) blinkAfter(shut bool) tea.Cmd {
	d := blinkShut
	if !shut {
		d = time.Duration(3000+rand.IntN(4000)) * time.Millisecond
	}
	msg := blinkMsg{gen: h.gen, shut: !shut}
	return tea.Tick(d, func(time.Time) tea.Msg { return msg })
}

func (h *home) Crumbs() []string { return nil }
func (h *home) Status() string   { return h.data.Status }
func (h *home) Command() string  { return "" }
func (h *home) Typing() bool     { return h.input.Value() != "" }

func (h *home) Keys() []Key {
	if h.Typing() {
		return []Key{{symUpDown, "выбор"}, {"enter", "открыть"}, {"esc", "очистить"}}
	}
	return []Key{{symUpDown, "выбор"}, {"enter", "открыть"}, {"буква enter", "раздел"}, {":", "команда"}, {"?", "клавиши"}, {"esc", "выход"}}
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
			h.list.Rows = append(h.list.Rows, Row{Text: "", Render: func(sel bool, w int) string {
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
	h.list.Cursor = 0
	h.skipHeaders(1)
}

// skipHeaders: курсор не останавливается на заголовках групп.
func (h *home) skipHeaders(dir int) {
	for h.list.Cursor >= 0 && h.list.Cursor < len(h.list.Rows) && h.list.Rows[h.list.Cursor].Text == "" {
		next := h.list.Cursor + dir
		if next < 0 || next >= len(h.list.Rows) {
			dir = -dir
			next = h.list.Cursor + dir
			if next < 0 || next >= len(h.list.Rows) {
				break
			}
		}
		h.list.Cursor = next
	}
}

func (h *home) current() *Hit {
	n := 0
	for i, r := range h.list.Rows {
		if r.Text == "" {
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
	case blinkMsg:
		if msg.gen != h.gen {
			return nil
		}
		h.blink = msg.shut
		return h.blinkAfter(msg.shut)
	case tea.KeyPressMsg:
		k := msg.String()
		switch k {
		case "up", "ctrl+p":
			h.list.Up()
			h.skipHeaders(-1)
			return nil
		case "down", "ctrl+n", "tab":
			h.list.Down()
			h.skipHeaders(1)
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
	// На низком терминале вторая строка плиток вытеснила бы предупреждение - остаётся одна.
	tileLines := tileRows(h.data.Tiles, cw, height >= 30)
	out = append(out, head...)
	switch {
	case !typing && wide:
		// Недавнее слева, разделы справа; заголовок группы у недавнего уже есть в списке.
		left := h.list.View(recentW, len(h.list.Rows))
		if len(left) == 0 {
			left = []string{"  " + styleHead.Render("НЕДАВНЕЕ"), "  " + styleHint.Render("проекты и команды, которые открывал, появятся здесь")}
		}
		right := append([]string{styleHead.Render("РАЗДЕЛЫ")}, secs...)
		out = append(out, Beside(left, recentW, right, cw)...)
	case !typing:
		// Друг под другом экран выше: недавнего столько, чтобы плитки и предупреждение остались видны.
		fit := height - len(head) - (len(secs) + 2) - len(tileLines) - 5
		out = append(out, h.list.View(cw, min(max(3, fit), len(h.list.Rows)))...)
		out = append(out, "", "  "+styleHead.Render("РАЗДЕЛЫ"))
		for _, sc := range secs {
			out = append(out, "  "+sc)
		}
	default:
		out = append(out, h.list.View(cw, max(3, height-len(head)-2))...)
	}
	if !typing {
		out = append(out, "")
		out = append(out, tileLines...)
		if h.data.Warning != "" {
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

// tileRows: плитки в строку, пока влезают; остальные - на следующую, если wrap, иначе не рисуются.
func tileRows(tiles []TileData, width int, wrap bool) []string {
	var out []string
	var row [][]string
	total := 2
	flush := func() {
		for i := 0; len(row) > 0 && i < len(row[0]); i++ {
			line := "  "
			for _, t := range row {
				line += t[i] + " "
			}
			out = append(out, line)
		}
		row, total = nil, 2
	}
	for _, t := range tiles {
		if total+t.Width+1 > width && len(row) > 0 {
			if !wrap {
				break
			}
			flush()
		}
		if total+t.Width+1 > width {
			continue
		}
		row = append(row, Tile(t.Title, t.Rows, t.Width))
		total += t.Width + 1
	}
	flush()
	return out
}
