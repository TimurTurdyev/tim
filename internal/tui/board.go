package tui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/TimurTurdyev/tim/internal/fuzzy"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// Доска: список строк с шапкой колонок и группами, карточка выбранной строки, действия буквами. На
// ней живут экраны без своей логики: Сеть, Сайты, Серверы, Утилиты. Данные и действия приходят из
// разделов. Действия над строкой - капы в её карточке, над экраном - в нижней строке.

// BoardRow: строка доски. Cells уже с цветом, полосу выбора доска добавит сама. Card - карточка
// справа или под списком; у строки-прозы без действий вместо неё Inline - одна строка со стрелкой под ней.
type BoardRow struct {
	ID     string
	Cells  string
	Group  string // заголовок группы, к которой строка относится, со счётчиком
	Card   *Card
	Inline string
	Find   string // по чему искать, если не по ID
	// Title, Sub и Pane: карточка по-старому - заголовок и строки как есть.
	Title string
	Sub   string
	Pane  []string
}

// BoardData: всё, что показывает доска за один раз.
type BoardData struct {
	Status  string
	Tab     string   // название вкладки со счётчиком, если доска - вкладка
	Head    []string // над списком: полосы, сводка
	Lamps   []string // короткие состояния под шапкой: по два в ряд, если влезают
	Columns string   // шапка таблицы прямо над строками
	Wide    bool     // строки шире 56 колонок: карточка всегда под списком
	Rows    []BoardRow
	Foot    []string // под списком: замечания
	Empty   string   // что сказать, если строк нет
}

// BoardAction: действие по букве. Do открывает экран или выходит; Run делает в фоне со строкой
// состояния, потом доска перечитывается. Free - действие над экраном, оно в нижней строке; остальные
// - над строкой, они в её карточке. Note - последствие у капа: "закроет tim", "sudo, закроет tim".
type BoardAction struct {
	Key, Label string
	Doing      string // что пишет строка состояния, пока идёт Run; пусто - подпись
	Note       string
	Do         func(row *BoardRow) Action
	Run        func(row *BoardRow) (string, error)
	Free       bool
	Hidden     bool // только в справке "?": нужно редко
}

// BoardOpts: источники доски.
type BoardOpts struct {
	Crumbs  []string
	Command func(row *BoardRow) string
	Load    func(fresh bool) BoardData // fresh - по r, иначе можно отдать из кэша
	Loading string                     // что сказать, пока первая загрузка идёт
	// Wait: заметка на время первой загрузки вместо строки Loading - например этапы сбора.
	Wait    func(width int) []string
	Actions []BoardAction
	Every   time.Duration // перечитывать само; 0 - только по r
	Static  bool          // данные не меняются: без r
	Search  bool          // "/" ищет по строкам
	Tab     string        // название вкладки, пока данных нет
	Order   string        // как отсортировано - справа в строке управления
	Refresh bool          // первая загрузка - из кэша, сразу за ней свежая
	// Miss: что сделать с запросом, которого нет в списке - например проверить его как домен.
	Miss  func(q string) (string, Action)
	Enter func(row *BoardRow) string
	// Palette: своя группа палитры ":" - заголовок и строки; см. Paletter.
	Palette func() (string, []CommandEntry)
}

type board struct {
	opts    BoardOpts
	data    BoardData
	shown   []int // строки данных в списке по порядку
	list    List
	input   textinput.Model
	have    bool
	loading bool
	inTabs  bool // строку поиска рисуют вкладки
	fresh   bool // свежая загрузка после кэша уже шла
}

type (
	boardDataMsg struct {
		data BoardData
	}
	boardTickMsg struct{}
)

// NewBoard создаёт доску.
func NewBoard(o BoardOpts) Screen {
	in := textinput.New()
	in.Prompt = symPointer + " "
	in.Placeholder = "найти"
	in.SetWidth(30)
	st := in.Styles()
	st.Focused.Prompt = stylePrompt
	st.Focused.Placeholder = styleHint
	st.Cursor.Color = colAccent
	in.SetStyles(st)
	in.Blur()
	return &board{opts: o, input: in}
}

func (b *board) Init() tea.Cmd { return tea.Batch(b.load(false), b.tick()) }

func (b *board) load(fresh bool) tea.Cmd {
	if b.loading {
		return nil
	}
	b.loading = true
	return func() tea.Msg { return boardDataMsg{b.opts.Load(fresh)} }
}

func (b *board) tick() tea.Cmd {
	if b.opts.Every <= 0 {
		return nil
	}
	return tea.Tick(b.opts.Every, func(time.Time) tea.Msg { return boardTickMsg{} })
}

func (b *board) Crumbs() []string { return b.opts.Crumbs }

// TabName: доска во вкладке.
func (b *board) TabName() string {
	if b.data.Tab != "" {
		return b.data.Tab
	}
	if b.opts.Tab != "" {
		return b.opts.Tab
	}
	if len(b.opts.Crumbs) > 0 {
		return b.opts.Crumbs[len(b.opts.Crumbs)-1]
	}
	return ""
}

func (b *board) Status() string {
	switch {
	case !b.have:
		return spinFrames[0] + " смотрю"
	case b.loading && b.data.Status != "":
		return spinFrames[0] + " " + b.data.Status + " " + symSep + " обновляю"
	case b.loading:
		return spinFrames[0] + " обновляю"
	}
	return b.data.Status
}

func (b *board) Palette() (string, []CommandEntry) {
	if b.opts.Palette == nil {
		return "", nil
	}
	return b.opts.Palette()
}

func (b *board) Command() string {
	if b.opts.Command == nil {
		return ""
	}
	return b.opts.Command(b.current())
}

func (b *board) Typing() bool { return b.input.Focused() }

// Escape: первый esc уходит из поиска, второй его очищает.
func (b *board) Escape() bool {
	switch {
	case b.input.Focused():
		b.input.Blur()
	case b.input.Value() != "":
		b.input.SetValue("")
		b.refill()
	default:
		return false
	}
	return true
}

func (b *board) setInTabs() { b.inTabs = true }

func (b *board) OrderText() string { return b.opts.Order }

// SearchLine: строка поиска в строке вкладок или над списком.
func (b *board) SearchLine() string {
	if !b.opts.Search {
		return ""
	}
	if b.input.Focused() || b.input.Value() != "" {
		return b.input.View()
	}
	return styleHint.Render(symPointer + " найти")
}

func (b *board) Keys() []Key {
	if b.input.Focused() {
		return []Key{{"enter", "готово"}, {"esc", "очистить"}}
	}
	if !b.have {
		if b.opts.Static {
			return nil
		}
		return []Key{{"r", "обновить"}}
	}
	var k []Key
	for _, a := range b.opts.Actions {
		if (a.Key == "enter" && b.current() != nil || a.Free) && !a.Hidden {
			k = append(k, Key{a.Key, a.Label})
		}
	}
	if b.opts.Search && len(b.data.Rows) > 0 {
		k = append(k, Key{"/", "найти"})
	}
	if !b.opts.Static {
		k = append(k, Key{"r", "обновить"})
	}
	return k
}

// HelpKeys: в справке - и редкие действия экрана, и действия строки.
func (b *board) HelpKeys() []Key {
	k := b.Keys()
	for _, a := range b.opts.Actions {
		if a.Hidden || !a.Free && a.Key != "enter" {
			k = append(k, Key{a.Key, a.Label})
		}
	}
	return k
}

func (b *board) current() *BoardRow {
	if i := b.rowAt(b.list.Cursor); i >= 0 {
		return &b.data.Rows[i]
	}
	return nil
}

// rowAt: строка данных под строкой списка; заголовок группы - -1.
func (b *board) rowAt(listIdx int) int {
	if listIdx < 0 || listIdx >= len(b.list.Rows) || b.list.Rows[listIdx].Header {
		return -1
	}
	n := 0
	for i := 0; i < listIdx; i++ {
		if !b.list.Rows[i].Header {
			n++
		}
	}
	if n < len(b.shown) {
		return b.shown[n]
	}
	return -1
}

func (b *board) refill() {
	cur := ""
	if c := b.current(); c != nil {
		cur = c.ID
	}
	b.shown = b.shown[:0]
	if q := strings.TrimSpace(b.input.Value()); q != "" {
		texts := make([]string, len(b.data.Rows))
		for i, r := range b.data.Rows {
			texts[i] = r.ID + " " + r.Find
		}
		for _, m := range fuzzy.Filter(q, texts) {
			b.shown = append(b.shown, m.Index)
		}
	} else {
		for i := range b.data.Rows {
			b.shown = append(b.shown, i)
		}
	}
	b.list.Rows = nil
	group := ""
	for _, i := range b.shown {
		r := b.data.Rows[i]
		if r.Group != "" && r.Group != group {
			group = r.Group
			b.list.Rows = append(b.list.Rows, Row{Header: true, Text: group, Render: func(bool, int) string {
				return "   " + styleHead.Render(r.Group)
			}})
		}
		b.list.Rows = append(b.list.Rows, Row{Text: r.ID, Render: func(sel bool, w int) string { return SelRow(sel, w, r.Cells) }})
	}
	// Курсор остаётся на той же строке, даже если она уехала по списку.
	b.list.Cursor = min(b.list.Cursor, max(0, len(b.list.Rows)-1))
	for li := range b.list.Rows {
		if i := b.rowAt(li); i >= 0 && b.data.Rows[i].ID == cur && cur != "" {
			b.list.Cursor = li
		}
	}
	b.list.Skip(1)
}

func (b *board) Update(msg tea.Msg, app *App) tea.Cmd {
	switch msg := msg.(type) {
	case boardDataMsg:
		b.data, b.have, b.loading = msg.data, true, false
		b.refill()
		if b.opts.Refresh && !b.fresh {
			b.fresh = true
			return b.load(true)
		}
		return nil
	case boardTickMsg:
		return tea.Batch(b.load(false), b.tick())
	case JobDone:
		return b.load(false)
	case tea.KeyPressMsg:
		return b.key(msg, app)
	}
	return nil
}

func (b *board) key(msg tea.KeyPressMsg, app *App) tea.Cmd {
	k := msg.String()
	if b.input.Focused() {
		if k == "enter" {
			b.input.Blur()
			if q := strings.TrimSpace(b.input.Value()); len(b.shown) == 0 && b.opts.Miss != nil && q != "" {
				_, act := b.opts.Miss(q)
				return app.Do(act)
			}
			return nil
		}
		var cmd tea.Cmd
		before := b.input.Value()
		b.input, cmd = b.input.Update(msg)
		if b.input.Value() != before {
			b.refill()
		}
		return cmd
	}
	if b.list.Key(k, 10) {
		return nil
	}
	switch {
	case k == "/" && b.opts.Search:
		return b.input.Focus()
	case k == "r" && !b.opts.Static:
		return b.load(true)
	}
	row := b.current()
	for _, a := range b.opts.Actions {
		if a.Key != k || (row == nil && !a.Free) {
			continue
		}
		switch {
		case a.Run != nil:
			doing := a.Doing
			if doing == "" {
				doing = a.Label
			}
			return app.Job(doing, func() (string, error) { return a.Run(row) })
		case a.Do != nil:
			return app.Do(a.Do(row))
		}
	}
	return nil
}

// card: карточка выбранной строки; действия над строкой - её капы.
func (b *board) card(row *BoardRow) *Card {
	if row == nil {
		return nil
	}
	c := row.Card
	if c == nil && row.Title != "" {
		c = &Card{Title: row.Title, Sub: row.Sub, Extra: row.Pane}
	}
	if c == nil {
		return nil
	}
	out := *c
	if out.Enter == "" && b.opts.Enter != nil {
		out.Enter = b.opts.Enter(row)
	}
	if len(out.Actions) == 0 {
		for _, a := range b.opts.Actions {
			if !a.Free && !a.Hidden && a.Key != "enter" {
				out.Actions = append(out.Actions, CardAction{a.Key, a.Label, a.Note})
			}
		}
	}
	return &out
}

func (b *board) View(width, height int) string {
	out := []string{""}
	if b.opts.Search && !b.inTabs {
		out = []string{b.controlLine(width), ""}
	}
	if !b.have && b.opts.Wait != nil {
		return strings.Join(b.opts.Wait(width), "\n")
	}
	if !b.have {
		text := b.opts.Loading
		if text == "" {
			text = "смотрю"
		}
		for _, l := range ui.Wrap(text, width-6) {
			out = append(out, "   "+styleDim.Render(l))
		}
		return strings.Join(out, "\n")
	}
	out = append(out, b.head(width)...)
	if len(b.data.Rows) == 0 {
		// Пусто - заметка: заголовок до двоеточия, пояснение - после.
		title, rest, _ := strings.Cut(b.data.Empty, ": ")
		out = append(out[:len(out)-len(b.head(width))], Notice(title, styleDim.Render(rest), "", width)...)
		if len(b.data.Head) > 0 || len(b.data.Lamps) > 0 {
			out = append(append(out, ""), b.head(width)...)
		}
		return strings.Join(out, "\n")
	}
	if b.data.Columns != "" {
		out = append(out, "   "+styleHead.Render(b.data.Columns))
	}
	if len(b.shown) == 0 {
		q := strings.TrimSpace(b.input.Value())
		line := fmt.Sprintf("ничего похожего на \"%s\" из %d", q, len(b.data.Rows))
		if b.opts.Miss != nil {
			text, _ := b.opts.Miss(q)
			line += " " + symSep + " enter " + text
		}
		out = append(out, "   "+styleDim.Render(line))
		return strings.Join(out, "\n")
	}
	var foot []string
	for _, f := range b.data.Foot {
		for _, l := range ui.Wrap(f, width-6) {
			foot = append(foot, "   "+l)
		}
	}
	room := height - len(out)
	if len(foot) > 0 {
		room -= len(foot) + 1
	}
	cw, beside := cardWidth(width)
	if b.data.Wide {
		cw, beside = width-6, false
	}
	card := b.card(b.current())
	switch {
	case card != nil && beside:
		list := b.listLines(cardListW, room)
		lines := card.Lines(cw, room)
		out = append(out, besideList(list, lines, width)...)
	case card != nil:
		// Под списком: короткому списку - сколько нужно, карточке - остальное; длинному - карточка
		// не выше трети тела.
		need := len(b.list.Rows) + 1
		cardH := min(len(card.Lines(cw, room)), max(5, room/3, room-need-1))
		list := b.listLines(width, room-cardH-1)
		out = append(out, list...)
		out = append(out, "")
		for _, l := range card.Lines(cw, cardH) {
			out = append(out, "   "+l)
		}
	default:
		out = append(out, b.listLines(width, room)...)
	}
	if len(foot) > 0 {
		out = append(append(out, ""), foot...)
	}
	return strings.Join(out, "\n")
}
