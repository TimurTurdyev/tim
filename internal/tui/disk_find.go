package tui

import (
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/TimurTurdyev/tim/internal/level"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// Поиск на Диске: по всему замеру, а не по вкладке - имя модели находит все её копии, "зависимости" -
// все node_modules и venv. Найденное отмечается разом и убирается тем же x.

// staleAfter: место, которое не трогали дольше, подсвечивается - первый кандидат на уборку.
const staleAfter = 180 * 24 * time.Hour

// searchInput: строка поиска, как у досок.
func searchInput() textinput.Model {
	in := textinput.New()
	in.Prompt = symPointer + " "
	in.Placeholder = "найти: имя папки, модели или места"
	in.SetWidth(40)
	st := in.Styles()
	st.Focused.Prompt = stylePrompt
	st.Focused.Placeholder = styleHint
	st.Cursor.Color = colAccent
	in.SetStyles(st)
	in.Blur()
	return in
}

// searching: показывается найденное, а не вкладка.
func (d *disk) searching() bool { return strings.TrimSpace(d.input.Value()) != "" }

func (d *disk) Typing() bool { return d.input.Focused() }

// Escape: первый esc уходит из строки поиска, второй убирает запрос и возвращает вкладку.
func (d *disk) Escape() bool {
	switch {
	case d.input.Focused():
		d.input.Blur()
	case d.searching():
		d.input.SetValue("")
		d.refind()
	default:
		return false
	}
	return true
}

// refind: найденное по запросу заново - после ввода и после нового замера.
func (d *disk) refind() {
	d.found = nil
	if q := strings.TrimSpace(d.input.Value()); q != "" && d.opts.Find != nil && !d.loading {
		d.found = d.opts.Find(q)
	}
	d.list.Cursor = 0
	d.refill()
}

func (d *disk) typeKey(msg tea.KeyPressMsg) tea.Cmd {
	if msg.String() == "enter" {
		d.input.Blur()
		return nil
	}
	before := d.input.Value()
	var cmd tea.Cmd
	d.input, cmd = d.input.Update(msg)
	if d.input.Value() != before {
		d.refind()
	}
	return cmd
}

// markFound: отметить всё найденное, что можно в Корзину; всё уже отмечено - снять.
func (d *disk) markFound(app *App) tea.Cmd {
	var can []DiskRow
	all := true
	for _, r := range d.found {
		if r.Removable && r.Path != "" {
			can = append(can, r)
			all = all && d.marked.has(r.Path)
		}
	}
	if len(can) == 0 {
		return app.Say(level.Warn, "из найденного в Корзину целиком нечего: как освободить, сказано в карточках")
	}
	for _, r := range can {
		if d.marked.has(r.Path) == all {
			d.marked.toggle(r.Path, r)
		}
	}
	return nil
}

// foundLine: строка поиска и итог найденного вместо вкладок.
func (d *disk) foundLine() string {
	line := d.input.View()
	if !d.searching() {
		return line
	}
	var sum int64
	for _, r := range d.found {
		sum += r.Size
	}
	count := ui.Count(len(d.found), "место", "места", "мест")
	if len(d.found) == 0 {
		count = "ничего не нашлось"
	}
	return line + "   " + styleDim.Render(count+" "+symSep+" "+ui.Bytes(sum))
}

// touched: давность изменения в колонке ТРОГАЛИ; давнее - янтарным.
func touched(t time.Time) string {
	if t.IsZero() {
		return ui.Pad("", 9)
	}
	d := time.Since(t)
	text := ui.Short(d)
	if d < 24*time.Hour {
		text = "сегодня"
	}
	if d >= staleAfter {
		return styleAmber.Render(ui.Pad(text, 9))
	}
	return styleDim.Render(ui.Pad(text, 9))
}
