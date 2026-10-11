package tui

import (
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/TimurTurdyev/tim/internal/ui"
)

// Главный экран: строка поиска, недавнее и разделы, плитки состояния, одно предупреждение.

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
	Art      []string // маскот, 6 строк по 12 клеток, уже с цветом
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
}

type homeDataMsg HomeData

// NewHome создаёт главный экран.
func NewHome(opts HomeOpts) Screen {
	in := textinput.New()
	in.Prompt = symPointer + " "
	in.Placeholder = "проект, команда, домен, порт - начни печатать"
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
	return tea.Batch(textinput.Blink, func() tea.Msg { return homeDataMsg(h.opts.Load()) })
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
			tw := 32
			if w < 70 {
				tw = 18
			}
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
		"  " + styleTitle.Render("Что сделать?") + "  " + styleHint.Render("проект, команда, домен, порт - начни печатать"),
		"  " + h.input.View(),
		"",
	}
	art := h.data.Art
	for i, l := range head {
		if i < len(art) && width >= 90 {
			out = append(out, ui.Fit(l, width-14)+art[i])
		} else {
			out = append(out, l)
		}
	}
	typing := h.Typing()
	wide := width >= 90 && len(h.data.Sections) > 0
	var secs []string
	for _, sc := range h.data.Sections {
		secs = append(secs, styleKeyCap.Render(" "+sc.Key+" ")+" "+ui.Fit(styleText.Render(sc.Title), 13)+styleDim.Render(sc.Hint))
	}
	switch {
	case !typing && wide:
		// Недавнее слева, разделы справа; заголовок группы у недавнего уже есть в списке.
		left := h.list.View(52, len(h.list.Rows))
		if len(left) == 0 {
			left = []string{"  " + styleHead.Render("НЕДАВНЕЕ"), "  " + styleHint.Render("проекты, в которые заходил, появятся здесь")}
		}
		right := append([]string{styleHead.Render("РАЗДЕЛЫ")}, secs...)
		rows := Beside(left, 52, right, width)
		// Строки рядом с картинкой собираются сразу уже: обрезка готовой строки ставила "~" на хвост из пробелов.
		narrow := Beside(left, 52, right, width-14)
		for i := range rows {
			if i+len(head) < len(art) {
				rows[i] = narrow[i] + art[i+len(head)]
			}
		}
		out = append(out, rows...)
	case !typing:
		out = append(out, h.list.View(width, len(h.list.Rows))...)
		out = append(out, "", "  "+styleHead.Render("РАЗДЕЛЫ"))
		for _, sc := range secs {
			out = append(out, "  "+sc)
		}
	default:
		out = append(out, h.list.View(width, max(3, height-len(head)-2))...)
	}
	if !typing {
		out = append(out, "")
		var tiles [][]string
		total := 2
		for _, t := range h.data.Tiles {
			if total+t.Width+1 > width {
				break
			}
			tiles = append(tiles, Tile(t.Title, t.Rows, t.Width))
			total += t.Width + 1
		}
		if len(tiles) > 0 {
			for i := 0; i < len(tiles[0]); i++ {
				line := "  "
				for _, t := range tiles {
					line += t[i] + " "
				}
				out = append(out, line)
			}
		}
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
	return strings.Join(out, "\n")
}
