package tui

import (
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/TimurTurdyev/tim/internal/fuzzy"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// Наложения поверх экрана: подтверждение, справка, командная строка.

// Dialog: окно подтверждения. Что именно, какой командой, можно ли вернуть - и одно действие.
type Dialog struct {
	Title   string
	Lines   []string // что будет сделано
	Command string   // какой командой
	Note    string   // например "из Корзины можно вернуть"
	OK      string   // подпись кнопки
	Danger  bool     // красная кнопка
	Yes     func() tea.Cmd
}

type confirmOverlay struct {
	d     Dialog
	onYes bool // выбрана ли кнопка действия; по умолчанию - отмена, она безопасная
}

func (c *confirmOverlay) update(msg tea.KeyPressMsg, app *App) tea.Cmd {
	switch msg.String() {
	case "esc", "n", "т":
		app.overlay = nil
	case "tab", "left", "right", "h", "l":
		c.onYes = !c.onYes
	case "y", "н":
		app.overlay = nil
		return c.d.Yes()
	case "enter":
		app.overlay = nil
		if c.onYes {
			return c.d.Yes()
		}
	}
	return nil
}

func (c *confirmOverlay) keys() []Key {
	return []Key{{"enter", "выбранная кнопка"}, {"tab", "другая кнопка"}, {"y", "да"}, {"esc", "отмена"}}
}

func (c *confirmOverlay) view(width, height int) string {
	w := min(width-4, 92)
	rows := []string{""}
	for _, l := range c.d.Lines {
		rows = append(rows, styleText.Render(l))
	}
	if c.d.Command != "" {
		rows = append(rows, "", styleDim.Render("Выполнится:  "+c.d.Command))
	}
	if c.d.Note != "" {
		rows = append(rows, styleDim.Render(c.d.Note))
	}
	ok := c.d.OK
	if ok == "" {
		ok = "Да"
	}
	btnYes, btnNo := styleBtnQuiet.Render(ok), styleBtnQuiet.Render("Отмена")
	if c.onYes {
		if c.d.Danger {
			btnYes = styleBtnDanger.Render(ok)
		} else {
			btnYes = styleBtnMain.Render(ok)
		}
	} else {
		btnNo = styleBtnMain.Render("Отмена")
	}
	rows = append(rows, "", btnYes+"   "+btnNo, "")
	box := Pane(c.d.Title, "", rows, w)
	pad := strings.Repeat(" ", max(0, (width-w)/2))
	top := max(0, (height-len(box))/2)
	var out []string
	for i := 0; i < top; i++ {
		out = append(out, "")
	}
	for _, l := range box {
		out = append(out, pad+l)
	}
	return strings.Join(out, "\n")
}

// ---------- справка ----------

type helpOverlay struct{ ks []Key }

func (h *helpOverlay) update(msg tea.KeyPressMsg, app *App) tea.Cmd {
	app.overlay = nil
	return nil
}

func (h *helpOverlay) keys() []Key { return []Key{{"любая клавиша", "закрыть"}} }

func (h *helpOverlay) view(width, height int) string {
	box := HelpBox(h.ks, width)
	return "\n" + strings.Join(box, "\n")
}

// HelpBox: клавиши экрана в три колонки и общие внизу.
func HelpBox(screenKeys []Key, width int) []string {
	w := min(width-4, 96)
	cols := 3
	col := (w - 4) / cols
	var rows []string
	rows = append(rows, "")
	for i := 0; i < len(screenKeys); i += cols {
		line := ""
		for j := i; j < i+cols && j < len(screenKeys); j++ {
			k := screenKeys[j]
			line += ui.Fit(styleKeyCap.Render(" "+k.K+" ")+"  "+styleText.Render(k.D), col)
		}
		rows = append(rows, line)
	}
	rows = append(rows, "")
	var g []string
	for _, k := range globalKeys {
		g = append(g, k.K+" "+k.D)
	}
	rows = append(rows, styleDim.Render("Везде:  "+strings.Join(g, "  "+symDot+"  ")))
	rows = append(rows, styleDim.Render("Убрать - всегда в Корзину и с подтверждением. Команда для терминала - внизу справа."), "")
	return Pane("Клавиши этого экрана", "", rows, w)
}

// ---------- командная строка ----------

type cmdline struct {
	input textinput.Model
	list  List
	hits  []CommandEntry
}

func newCmdline(app *App) *cmdline {
	in := textinput.New()
	in.Prompt = ": "
	in.Placeholder = "команда tim, например serve shop -d"
	in.SetWidth(60)
	st := in.Styles()
	st.Focused.Prompt = stylePrompt
	st.Focused.Placeholder = styleHint
	st.Cursor.Color = colAccent
	in.SetStyles(st)
	in.Focus()
	c := &cmdline{input: in}
	c.refill(app)
	return c
}

func (c *cmdline) refill(app *App) {
	c.hits = nil
	if app.opts.Commands != nil {
		all := app.opts.Commands(c.input.Value())
		if q := strings.TrimSpace(c.input.Value()); q != "" {
			names := make([]string, len(all))
			for i, e := range all {
				names[i] = e.Line
			}
			for _, m := range fuzzy.Filter(q, names) {
				c.hits = append(c.hits, all[m.Index])
			}
		} else {
			c.hits = all
		}
	}
	if len(c.hits) > 8 {
		c.hits = c.hits[:8]
	}
	c.list.Rows = nil
	for _, h := range c.hits {
		h := h
		c.list.Rows = append(c.list.Rows, Row{Text: h.Line, Render: func(sel bool, w int) string {
			return SelRow(sel, w, ui.Fit(styleBold.Render(h.Line), 34)+styleDim.Render(h.Desc))
		}})
	}
	c.list.Cursor = 0
}

func (c *cmdline) update(msg tea.KeyPressMsg, app *App) tea.Cmd {
	switch msg.String() {
	case "esc":
		app.overlay = nil
		return nil
	case "up", "down":
		c.list.Key(msg.String(), 8)
		return nil
	case "tab":
		if c.list.Cursor < len(c.hits) {
			c.input.SetValue(c.hits[c.list.Cursor].Line)
			c.input.CursorEnd()
			c.refill(app)
		}
		return nil
	case "enter":
		line := strings.TrimSpace(c.input.Value())
		if line == "" && c.list.Cursor < len(c.hits) {
			line = c.hits[c.list.Cursor].Line
		}
		app.overlay = nil
		if line == "" || app.opts.RunCommand == nil {
			return nil
		}
		return app.Do(app.opts.RunCommand(line))
	}
	var cmd tea.Cmd
	before := c.input.Value()
	c.input, cmd = c.input.Update(msg)
	if c.input.Value() != before {
		c.refill(app)
	}
	return cmd
}

func (c *cmdline) keys() []Key {
	return []Key{{"tab", "подставить"}, {"enter", "выполнить"}, {"esc", "отмена"}}
}

func (c *cmdline) view(width, height int) string {
	rows := c.list.View(width, len(c.hits))
	var out []string
	for i := 0; i < max(0, height-len(rows)-3); i++ {
		out = append(out, "")
	}
	out = append(out, "  "+styleHead.Render("ПОДСКАЗКИ"))
	out = append(out, rows...)
	out = append(out, " "+c.input.View())
	return strings.Join(out, "\n")
}
