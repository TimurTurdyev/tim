package tui

import (
	"fmt"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/TimurTurdyev/tim/internal/level"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// Обзор диска: полоса занятости, разделы вкладками, места с размерами и карточка выбранного под
// списком. Места двух видов: папку отмечают и убирают в Корзину, кэш чистят его командой.

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
	Cmd                    string // кэш чистится этой командой: enter выполняет её через подтверждение
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

// DiskOpts: источники и действия обзора. progress - сколько папок посчитано из скольких и где.
type DiskOpts struct {
	Load      func(fresh bool, progress func(done, total int, where string)) DiskData
	Remove    func(paths []string) (freed int64, errs []string)
	Clean     func(cmd string) (string, error) // команда очистки кэша
	Open      func(path string)                // показать в Finder
	Browse    func() Screen
	Leftovers func() Screen
}

type disk struct {
	opts    DiskOpts
	data    DiskData
	tab     int
	list    List
	marked  marks[DiskRow]
	loading bool
	started time.Time
	done    int
	total   int
	where   string
	mu      sync.Mutex
}

type diskDataMsg DiskData

// NewDisk создаёт экран обзора диска.
func NewDisk(opts DiskOpts) Screen {
	return &disk{opts: opts}
}

func (d *disk) Init() tea.Cmd { return d.load(false) }

func (d *disk) load(fresh bool) tea.Cmd {
	d.loading, d.started = true, time.Now()
	return func() tea.Msg {
		return diskDataMsg(d.opts.Load(fresh, func(done, total int, where string) {
			d.mu.Lock()
			d.done, d.total, d.where = done, total, where
			d.mu.Unlock()
		}))
	}
}

// Animating: пока идёт замер, полоса хода и секунды движутся от общего тикера.
func (d *disk) Animating() bool { return d.loading }

func (d *disk) Tick(int) {}

// counted: сколько посчитано.
func (d *disk) counted() (done, total int, where string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.done, d.total, d.where
}

func (d *disk) Crumbs() []string { return []string{"Диск"} }

func (d *disk) Status() string {
	if d.loading {
		done, total, _ := d.counted()
		// Полоса хода в теле уже движется - статус без кадра, только счёт и секунды.
		return "считаю " + itoa(done) + " из " + itoa(total) + " " + symSep + " " + ui.Short(time.Since(d.started))
	}
	return ui.Bytes(d.data.Size-d.data.Avail) + " из " + ui.Bytes(d.data.Size) + " " + symSep + " свободно " + ui.Bytes(d.data.Avail)
}

func (d *disk) Command() string { return "tim disk" }

func (d *disk) Keys() []Key {
	if d.loading && len(d.data.Tabs) == 0 {
		return nil
	}
	enter := Key{"enter", "по папкам"}
	if cur := d.current(); cur != nil && cur.Cmd != "" {
		enter = Key{"enter", "выполнить"}
	}
	return []Key{enter, {"space", "отметить"}, {"x", "убрать"}, {"h", "хвосты"}, {"tab", "вкладка"}}
}

// HelpKeys: r и o - в справке: r подписан в строке полосы, o - в карточке.
func (d *disk) HelpKeys() []Key {
	return append(d.Keys(), Key{"r", "обновить"}, Key{"o", "Finder"})
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
		d.list.Rows = append(d.list.Rows, Row{Text: r.Name, Render: func(sel bool, w int) string {
			mark := " "
			if r.Path != "" && d.marked.has(r.Path) {
				mark = styleBad.Render(symCheck)
			}
			// Доля - от самого крупного места: процент от диска тут ничего не говорит.
			cells := max(1, int(r.Size*8/max(1, top)))
			bar := styleText.Render(strings.Repeat("\u2584", cells)) + styleGhost.Render(strings.Repeat("\u2582", 8-cells))
			nameW := min(34, max(20, w-60))
			name := styleText.Render(ui.Fit(r.Name, nameW))
			if sel {
				name = styleBold.Render(ui.Fit(r.Name, nameW))
			}
			tail := styleDim.Render(r.Hint)
			if r.Warn != "" {
				tail = styleAmber.Render(r.Warn)
			}
			line := fmt.Sprintf("%s%9s   %s  %s %s", mark, ui.Bytes(r.Size), bar, name, tail)
			return SelRow(sel, w, line)
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
	case JobDone:
		return d.load(false)
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
					return app.Say(level.Warn, cur.Name+": это не папка для Корзины - как убрать, сказано в карточке")
				}
				d.marked.toggle(cur.Path, *cur)
				d.list.Down()
			}
		case "o":
			if cur := d.current(); cur != nil && cur.Path != "" && d.opts.Open != nil {
				d.opts.Open(cur.Path)
				return app.Say(level.OK, cur.Name+": открыто в Finder")
			}
		case "x":
			return d.confirmRemove(app)
		case "h":
			if d.opts.Leftovers != nil {
				return app.Push(d.opts.Leftovers())
			}
		case "r":
			return d.load(true)
		case "enter":
			cur := d.current()
			switch {
			case cur != nil && cur.Cmd != "":
				return d.confirmClean(app, *cur)
			case d.opts.Browse != nil && !d.loading:
				return app.Push(d.opts.Browse())
			}
		}
	}
	return nil
}

func (d *disk) confirmRemove(app *App) tea.Cmd {
	if d.marked.len() == 0 {
		return app.Say(level.Warn, "сначала отметь пробелом, что убрать")
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
			d.marked.clear()
			what := "убираю в Корзину " + ui.Count(len(paths), "место", "места", "мест") + ", " + ui.Bytes(sum)
			return app.Job(what, func() (string, error) {
				freed, errs := d.opts.Remove(paths)
				if len(errs) > 0 {
					return "", ui.Errorf("%s", strings.Join(errs, "; "))
				}
				return "Корзина: +" + ui.Bytes(freed) + ", место освободится после её очистки", nil
			})
		},
	})
}

// confirmClean: команда очистки кэша - через то же окно, что и Корзина, и в фоне.
func (d *disk) confirmClean(app *App, r DiskRow) tea.Cmd {
	note := "Кэш пересоздастся сам, когда понадобится."
	if r.Cmd == "очистить Корзину" {
		note = "Из очищенной Корзины ничего не вернуть."
	}
	return app.Confirm(Dialog{
		Title:   r.Name + ": очистить?",
		Lines:   []string{ui.Fit(ui.Bytes(r.Size), 9) + "  " + r.Name + "  " + styleDim.Render(r.shown())},
		Command: r.Cmd,
		Note:    note,
		OK:      "Выполнить",
		Danger:  true,
		Yes: func() tea.Cmd {
			return app.Job("выполняю "+r.Cmd, func() (string, error) { return d.opts.Clean(r.Cmd) })
		},
	})
}
