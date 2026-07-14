package tui

import (
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/TimurTurdyev/tim/internal/level"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// Обзор диска: полоса занятости, разделы вкладками, список мест и панель с подробностями.

// DiskItem: что лежит внутри места - модель, игра, виртуалка.
type DiskItem struct {
	Name, Note string
	Size       int64
	Stale      bool // давно не нужно: подпись желтым
}

// DiskRow: одно место на диске. Path - настоящий путь для уборки, Show - как его печатать.
type DiskRow struct {
	Name, Path, Show, Hint string
	Size                   int64
	Items                  []DiskItem
	Warn                   string // например "Steam удалён, а игры остались"
	Removable              bool   // можно убрать в Корзину целиком
}

func (r DiskRow) shown() string {
	if r.Show != "" {
		return r.Show
	}
	return r.Path
}

// DiskTab: раздел обзора.
type DiskTab struct {
	Title string
	Rows  []DiskRow
	Total int64
}

// DiskData: всё для экрана; собирается в разделе disk.
type DiskData struct {
	UsedPct     int
	Avail, Size int64
	Tabs        []DiskTab
	MeasuredAt  time.Time
	Notes       []string // например про копии Chrome
}

// DiskOpts: источники и действия обзора.
type DiskOpts struct {
	Load      func(fresh bool, progress func(string)) DiskData
	Remove    func(paths []string) (freed int64, errs []string)
	Browse    func() Screen
	Leftovers func() Screen
}

type disk struct {
	opts     DiskOpts
	data     DiskData
	tab      int
	list     List
	marked   marks[DiskRow]
	loading  bool
	progress string
	mu       sync.Mutex
}

type diskDataMsg DiskData
type diskTickMsg struct{}

// NewDisk создаёт экран обзора диска.
func NewDisk(opts DiskOpts) Screen {
	return &disk{opts: opts}
}

func (d *disk) Init() tea.Cmd { return d.load(false) }

func (d *disk) load(fresh bool) tea.Cmd {
	d.loading = true
	d.progress = "считаю..."
	return tea.Batch(
		func() tea.Msg {
			return diskDataMsg(d.opts.Load(fresh, func(s string) {
				d.mu.Lock()
				d.progress = s
				d.mu.Unlock()
			}))
		},
		tea.Tick(300*time.Millisecond, func(time.Time) tea.Msg { return diskTickMsg{} }),
	)
}

func (d *disk) Crumbs() []string { return []string{"Диск"} }

func (d *disk) Status() string {
	if d.loading {
		d.mu.Lock()
		defer d.mu.Unlock()
		return d.progress
	}
	return "занято " + ui.Bytes(d.data.Size-d.data.Avail) + " из " + ui.Bytes(d.data.Size) + " " + symDot + " свободно " + ui.Bytes(d.data.Avail)
}

func (d *disk) Command() string { return "tim disk" }

func (d *disk) Keys() []Key {
	return []Key{{symLR, "разделы"}, {symUpDown, "место"}, {"space", "отметить"}, {"x", "убрать"}, {"b", "по папкам"}, {"h", "хвосты программ"}, {"r", "пересчитать"}}
}

func (d *disk) rows() []DiskRow {
	if d.tab < len(d.data.Tabs) {
		return d.data.Tabs[d.tab].Rows
	}
	return nil
}

func (d *disk) refill() {
	rows := d.rows()
	var top int64
	for _, r := range rows {
		top = max(top, r.Size)
	}
	d.list.Rows = nil
	for _, r := range rows {
		r := r
		d.list.Rows = append(d.list.Rows, Row{Text: r.Name, Render: func(sel bool, w int) string {
			mark := "  "
			if r.Path != "" && d.marked.has(r.Path) {
				mark = styleBad.Render(symCheck) + " "
			}
			size := styleText.Render(ui.Fit(ui.Bytes(r.Size), 9))
			bar := styleAccent.Render(strings.Repeat(symBlock, max(1, int(r.Size*8/max(1, top))))) +
				styleGhost.Render(strings.Repeat(symShade, 8-max(1, int(r.Size*8/max(1, top)))))
			name := styleText.Render(ui.Fit(r.Name, 34))
			if sel {
				name = styleBold.Render(ui.Fit(r.Name, 34))
			}
			tail := styleDim.Render(r.Hint)
			if r.Warn != "" {
				tail = styleAmber.Render(r.Warn)
			}
			return SelRow(sel, w, mark+size+" "+bar+"  "+name+" "+tail)
		}})
	}
	if d.list.Cursor >= len(d.list.Rows) {
		d.list.Cursor = max(0, len(d.list.Rows)-1)
	}
}

func (d *disk) current() *DiskRow {
	rows := d.rows()
	if d.list.Cursor < len(rows) {
		return &rows[d.list.Cursor]
	}
	return nil
}

func (d *disk) Update(msg tea.Msg, app *App) tea.Cmd {
	switch msg := msg.(type) {
	case diskDataMsg:
		d.data = DiskData(msg)
		d.loading = false
		if d.tab >= len(d.data.Tabs) {
			d.tab = 0
		}
		d.refill()
		return nil
	case diskTickMsg:
		if d.loading {
			return tea.Tick(300*time.Millisecond, func(time.Time) tea.Msg { return diskTickMsg{} })
		}
		return nil
	case tea.KeyPressMsg:
		k := msg.String()
		if d.list.Key(k, 10) {
			return nil
		}
		// h занят хвостами, поэтому вкладки листаются стрелками и tab.
		switch k {
		case "left", "shift+tab":
			d.tab = (d.tab + len(d.data.Tabs) - 1) % max(1, len(d.data.Tabs))
			d.list.Cursor = 0
			d.refill()
		case "right", "tab":
			d.tab = (d.tab + 1) % max(1, len(d.data.Tabs))
			d.list.Cursor = 0
			d.refill()
		case "space", " ":
			if cur := d.current(); cur != nil {
				if !cur.Removable || cur.Path == "" {
					return app.Toast(level.Warn, "это место так не убрать: смотри подсказку справа")
				}
				d.marked.toggle(cur.Path, *cur)
				d.list.Down()
			}
		case "x", "ч":
			return d.confirmRemove(app)
		case "b", "и":
			if d.loading {
				return app.Toast(level.Warn, "подожди, замер ещё идёт")
			}
			if d.opts.Browse != nil {
				return app.Push(d.opts.Browse())
			}
		case "h", "р":
			if d.opts.Leftovers != nil {
				return app.Push(d.opts.Leftovers())
			}
		case "r", "к":
			return d.load(true)
		case "enter":
			if cur := d.current(); cur != nil && d.opts.Browse != nil && cur.Path != "" && !d.loading {
				return app.Push(d.opts.Browse())
			}
		}
	}
	return nil
}

func (d *disk) confirmRemove(app *App) tea.Cmd {
	if d.marked.len() == 0 {
		return app.Toast(level.Warn, "сначала отметь пробелом, что убрать")
	}
	var lines, paths []string
	var sum int64
	for _, r := range d.marked.list() {
		lines = append(lines, ui.Fit(ui.Bytes(r.Size), 9)+"  "+r.Name+"  "+styleDim.Render(r.shown()))
		paths = append(paths, r.Path)
		sum += r.Size
	}
	return app.Confirm(Dialog{
		Title:   "Убрать в Корзину?",
		Lines:   lines,
		Command: "trash " + strings.Join(paths, " "),
		Note:    "Из Корзины можно вернуть. Место освободится после её очистки.",
		OK:      "Убрать " + ui.Bytes(sum),
		Danger:  true,
		Yes: func() tea.Cmd {
			freed, errs := d.opts.Remove(paths)
			d.marked.clear()
			var cmds []tea.Cmd
			if len(errs) > 0 {
				cmds = append(cmds, app.Toast(level.Bad, strings.Join(errs, "; ")))
			} else {
				cmds = append(cmds, app.Toast(level.OK, "в Корзине "+ui.Bytes(freed)+", место освободится после её очистки"))
			}
			cmds = append(cmds, d.load(false))
			return tea.Batch(cmds...)
		},
	})
}

func (d *disk) View(width, height int) string {
	var out []string
	if d.loading && len(d.data.Tabs) == 0 {
		out = append(out, "", "  "+styleEmpty.Render("считаю, куда ушло место..."), "",
			"  "+styleDim.Render("Первый замер домашней папки идёт около минуты, потом 15 минут берётся готовый."),
			"  "+styleDim.Render("Можно не ждать: esc - выйти, замер продолжится в фоне."))
		return strings.Join(out, "\n")
	}
	frac := float64(d.data.UsedPct) / 100
	out = append(out, "  "+Bar(frac, min(60, width-40))+"  "+pctStyle(d.data.UsedPct)+
		styleDim.Render("  замер "+ui.Ago(d.data.MeasuredAt)+", r - заново"))
	out = append(out, "")
	var tabs []string
	for i, t := range d.data.Tabs {
		label := " " + t.Title + " " + ui.Bytes(t.Total) + " "
		if i == d.tab {
			tabs = append(tabs, styleTabOn.Render(label))
		} else {
			tabs = append(tabs, styleTabOff.Render(label))
		}
	}
	out = append(out, "  "+strings.Join(tabs, " "), "")
	paneH := 0
	cur := d.current()
	var pane []string
	if cur != nil {
		var rows []string
		for i, it := range cur.Items {
			if i >= 6 {
				rows = append(rows, styleDim.Render("ещё "+itoa(len(cur.Items)-6)))
				break
			}
			note := styleDim.Render(it.Note)
			if it.Stale {
				note = styleAmber.Render(it.Note)
			}
			rows = append(rows, styleDim.Render(ui.Fit(ui.Bytes(it.Size), 9))+"  "+ui.Fit(it.Name, 32)+note)
		}
		if cur.Warn != "" {
			rows = append(rows, styleAmber.Render(symWarn+" "+cur.Warn))
		}
		if cur.Hint != "" {
			rows = append(rows, styleDim.Render(symArrow+" "+cur.Hint))
		}
		if len(rows) == 0 {
			rows = append(rows, styleDim.Render("подробностей нет"))
		}
		pane = Pane(cur.Name, cur.shown(), rows, width-4)
		paneH = len(pane)
	}
	notes := len(d.data.Notes)
	listH := max(3, height-len(out)-paneH-notes-2)
	out = append(out, d.list.View(width, listH)...)
	for len(out) < height-paneH-notes-1 {
		out = append(out, "")
	}
	if d.marked.len() > 0 {
		var sum int64
		for _, r := range d.marked.list() {
			sum += r.Size
		}
		out[len(out)-1] = "  " + styleBad.Render("отмечено "+itoa(d.marked.len())+" "+symDot+" "+ui.Bytes(sum)) + styleDim.Render("   x - убрать")
	}
	for _, p := range pane {
		out = append(out, "  "+p)
	}
	for _, n := range d.data.Notes {
		out = append(out, "  "+styleDim.Render(n))
	}
	return strings.Join(out, "\n")
}

func pctStyle(pct int) string {
	s := itoa(pct) + "%"
	switch {
	case pct >= 85:
		return styleBad.Render(s)
	case pct >= 60:
		return styleAmber.Render(s)
	}
	return styleText.Render(s)
}
