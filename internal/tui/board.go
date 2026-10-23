package tui

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
)

// Доска: список строк, панель выбранной и действия буквами. На ней живут экраны без своей
// логики: VPN и обход, dev-серверы, проверка проектов. Данные и действия приходят из cmd.

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
	Status string
	Head   []string // над списком: лампочки, шапка таблицы
	Rows   []BoardRow
	Foot   []string // под панелью: замечания
	Empty  string   // что сказать, если строк нет
}

// BoardAction: действие по букве. Do открывает экран или выходит; Run делает в фоне, результат
// тостом, потом доска перечитывается.
type BoardAction struct {
	Key, Label string
	Do         func(row *BoardRow) Action
	Run        func(row *BoardRow) (string, error)
}

// BoardOpts: источники доски.
type BoardOpts struct {
	Crumbs  []string
	Command func(row *BoardRow) string
	Load    func(fresh bool) BoardData // fresh - по r, иначе можно отдать из кэша
	Loading string                     // что сказать, пока первая загрузка идёт
	Actions []BoardAction
	Every   time.Duration // перечитывать само; 0 - только по r
}

type board struct {
	opts    BoardOpts
	data    BoardData
	list    List
	have    bool
	loading bool
	busy    string
}

type (
	boardDataMsg struct {
		data BoardData
	}
	boardTickMsg struct{}
	boardRunMsg  struct {
		text string
		err  error
	}
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
	case b.busy != "":
		return b.busy + "..."
	case !b.have:
		return "смотрю..."
	case b.loading:
		return b.data.Status + " " + symDot + " обновляю"
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
	k := []Key{{symUpDown, "выбор"}}
	for _, a := range b.opts.Actions {
		k = append(k, Key{a.Key, a.Label})
	}
	return append(k, Key{"r", "обновить"}, Key{"esc", "назад"})
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
	case boardRunMsg:
		b.busy = ""
		if msg.err != nil {
			return tea.Batch(app.Toast("bad", msg.err.Error()), b.load(false))
		}
		return tea.Batch(app.Toast("ok", msg.text), b.load(false))
	case tea.KeyPressMsg:
		k := msg.String()
		if b.list.Key(k, 10) {
			return nil
		}
		if k == "r" || k == "к" {
			return b.load(true)
		}
		for _, a := range b.opts.Actions {
			if a.Key != k {
				continue
			}
			if b.busy != "" {
				return app.Toast("warn", "подожди: "+b.busy)
			}
			row := b.current()
			switch {
			case a.Run != nil:
				b.busy = a.Label
				return func() tea.Msg {
					text, err := a.Run(row)
					return boardRunMsg{text, err}
				}
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
		out = append(out, "  "+styleEmpty.Render(text))
		return strings.Join(out, "\n")
	}
	for _, h := range b.data.Head {
		out = append(out, "  "+h)
	}
	if len(b.data.Head) > 0 {
		out = append(out, "")
	}
	var pane []string
	if cur := b.current(); cur != nil && cur.Title != "" {
		pane = Pane(cur.Title, cur.Sub, cur.Pane, width-4)
	}
	tail := len(pane) + len(b.data.Foot)
	if len(b.data.Foot) > 0 {
		tail++
	}
	listH := max(3, height-len(out)-tail-1)
	if len(b.data.Rows) == 0 {
		out = append(out, "  "+styleDim.Render(b.data.Empty))
	} else {
		out = append(out, b.list.View(width, listH)...)
	}
	for len(out) < height-tail {
		out = append(out, "")
	}
	for _, p := range pane {
		out = append(out, "  "+p)
	}
	if len(b.data.Foot) > 0 {
		out = append(out, "")
		for _, f := range b.data.Foot {
			out = append(out, "  "+f)
		}
	}
	return strings.Join(out, "\n")
}
