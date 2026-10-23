package tui

import (
	"fmt"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/TimurTurdyev/tim/internal/ui"
)

// Доска: список строк, панель выбранной и действия буквами. На ней живут экраны без своей
// логики: VPN и обход, dev-серверы, проверка проектов. Данные и действия приходят из разделов.

// BoardRow: строка доски. Cells уже с цветом, полосу выбора доска добавит сама.
type BoardRow struct {
	ID    string
	Cells string
	Title string // заголовок панели; пусто - панели нет
	Sub   string
	Pane  []string
}

// BoardData: всё, что показывает доска за один раз.
type BoardData struct {
	Status  string
	Head    []string // над списком: полосы, сводка
	Lamps   []string // короткие состояния под шапкой: по два в ряд, если влезают
	Columns string   // шапка таблицы прямо над списком
	Rows    []BoardRow
	Foot    []string // под панелью: замечания
	Empty   string   // что сказать, если строк нет
}

// BoardAction: действие по букве. Do открывает экран или выходит; Run делает в фоне, результат
// тостом, потом доска перечитывается.
type BoardAction struct {
	Key, Label string
	Doing      string // что пишет строка состояния, пока идёт Run; пусто - подпись
	Do         func(row *BoardRow) Action
	Run        func(row *BoardRow) (string, error)
	Free       bool // работает и без выбранной строки; остальные на пустой доске не показываются
}

// BoardOpts: источники доски.
type BoardOpts struct {
	Crumbs  []string
	Command func(row *BoardRow) string
	Load    func(fresh bool) BoardData // fresh - по r, иначе можно отдать из кэша
	Loading string                     // что сказать, пока первая загрузка идёт
	Actions []BoardAction
	Every   time.Duration // перечитывать само; 0 - только по r
	Static  bool          // данные не меняются: без r
}

type board struct {
	opts    BoardOpts
	data    BoardData
	list    List
	have    bool
	loading bool
}

type (
	boardDataMsg struct {
		data BoardData
	}
	boardTickMsg struct{}
)

// NewBoard создаёт доску.
func NewBoard(o BoardOpts) Screen { return &board{opts: o} }

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

func (b *board) Command() string {
	if b.opts.Command == nil {
		return ""
	}
	return b.opts.Command(b.current())
}

func (b *board) Keys() []Key {
	var k []Key
	for _, a := range b.opts.Actions {
		if a.Free || len(b.data.Rows) > 0 {
			k = append(k, Key{a.Key, a.Label})
		}
	}
	if !b.opts.Static {
		k = append(k, Key{"r", "обновить"})
	}
	return k
}

func (b *board) current() *BoardRow {
	if b.list.Cursor < len(b.data.Rows) {
		return &b.data.Rows[b.list.Cursor]
	}
	return nil
}

func (b *board) refill() {
	cur := ""
	if c := b.current(); c != nil {
		cur = c.ID
	}
	b.list.Rows = nil
	for i, r := range b.data.Rows {
		r := r
		b.list.Rows = append(b.list.Rows, Row{Text: r.ID, Render: func(sel bool, w int) string { return SelRow(sel, w, r.Cells) }})
		// Курсор остаётся на той же строке, даже если она уехала по списку.
		if r.ID == cur && cur != "" {
			b.list.Cursor = i
		}
	}
	if b.list.Cursor >= len(b.list.Rows) {
		b.list.Cursor = max(0, len(b.list.Rows)-1)
	}
}

func (b *board) Update(msg tea.Msg, app *App) tea.Cmd {
	switch msg := msg.(type) {
	case boardDataMsg:
		b.data, b.have, b.loading = msg.data, true, false
		b.refill()
		return nil
	case boardTickMsg:
		return tea.Batch(b.load(false), b.tick())
	case JobDone:
		return b.load(false)
	case tea.KeyPressMsg:
		k := msg.String()
		if b.list.Key(k, 10) {
			return nil
		}
		if k == "r" && !b.opts.Static {
			return b.load(true)
		}
		for _, a := range b.opts.Actions {
			row := b.current()
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
	}
	return nil
}

func (b *board) View(width, height int) string {
	out := []string{""}
	if !b.have {
		text := b.opts.Loading
		if text == "" {
			text = "смотрю..."
		}
		for _, l := range ui.Wrap(text, width-4) {
			out = append(out, "  "+styleEmpty.Render(l))
		}
		return strings.Join(out, "\n")
	}
	out = append(out, b.head(width)...)
	var footLines []string
	for _, f := range b.data.Foot {
		footLines = append(footLines, ui.Wrap(f, width-4)...)
	}
	foot := len(footLines)
	if foot > 0 {
		foot++
	}
	var pane []string
	if cur := b.current(); cur != nil && cur.Title != "" {
		// Панель не отнимает у списка последние строки: на низком экране её хвост сворачивается.
		room := height - len(out) - min(len(b.data.Rows), 4) - foot - 3
		var rows []string
		for _, r := range cur.Pane {
			rows = append(rows, ui.Wrap(r, width-8)...)
		}
		pane = Pane(cur.Title, cur.Sub, clipRows(rows, room), width-4)
	}
	tail := len(pane) + foot
	listH := max(3, height-len(out)-tail-1)
	if len(b.data.Rows) == 0 {
		for _, l := range ui.Wrap(b.data.Empty, width-4) {
			out = append(out, "  "+styleDim.Render(l))
		}
	} else {
		out = append(out, b.list.View(width, listH)...)
	}
	for len(out) < height-tail {
		out = append(out, "")
	}
	for _, p := range pane {
		out = append(out, "  "+p)
	}
	if foot > 0 {
		out = append(out, "")
		for _, f := range footLines {
			out = append(out, "  "+f)
		}
	}
	return strings.Join(out, "\n")
}

// head: шапка, лампы и названия колонок - всё, что над списком.
func (b *board) head(width int) []string {
	var out []string
	for _, h := range b.data.Head {
		out = append(out, "  "+h)
	}
	if len(b.data.Lamps) > 0 {
		if len(out) > 0 {
			out = append(out, "")
		}
		for _, l := range ui.Pairs(b.data.Lamps, width-4) {
			out = append(out, "  "+l)
		}
	}
	if len(out) > 0 {
		out = append(out, "")
	}
	if b.data.Columns != "" {
		// Три пробела, как у строк списка с полосой выбора: иначе шапка на колонку левее значений.
		out = append(out, "   "+b.data.Columns, "")
	}
	return out
}

// clipRows: не больше n строк; последняя говорит, сколько не влезло.
func clipRows(rows []string, n int) []string {
	if len(rows) <= n {
		return rows
	}
	if n < 1 {
		return nil
	}
	return append(slices.Clone(rows[:n-1]), styleDim.Render(fmt.Sprintf("и ещё %d", len(rows)-n+1)))
}
