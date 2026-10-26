package tui

import (
	"slices"
	"strings"
	"unicode"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/TimurTurdyev/tim/internal/fuzzy"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// Наложения поверх приглушённого экрана: подтверждение по центру, шпаргалка ящиком снизу, палитра
// ":" сверху. Экран под ними остаётся виден серым и на своих местах: решение принимается в контексте.

// Группы палитры по порядку.
const (
	groupError    = "ошибка"
	groupRecent   = "недавнее"
	groupSubst    = "подстановка"
	groupCommands = "команды"
	groupTemplate = "шаблоны"
)

// listKeys и everywhere: колонки шпаргалки, одинаковые на всех экранах.
var (
	listKeys  = []Key{{symUpDown + " j k", "строка"}, {"pgup pgdn", "страница"}, {"home end g G", "начало, конец"}}
	everyKeys = []Key{{":", "команда"}, {"?", "эта шпаргалка"}, {"esc", "назад"}, {"q", "выход"}}
)

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
	// Только enter, tab, esc и латинская y: раскладка здесь не переводится - "н" стоит на клавише y
	// и подтвердила бы удаление.
	switch msg.String() {
	case "esc":
		app.overlay = nil
	case "tab", "shift+tab", "left", "right":
		c.onYes = !c.onYes
	case "y":
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
	return []Key{{"enter", "выбранная кнопка"}, {"tab", "другая"}, {"esc", "отмена"}}
}

// compose: окно с рамкой по центру; строки экрана выше и ниже окна остаются видны.
func (c *confirmOverlay) compose(base []string, width, height int) []string {
	w := min(width-4, 92)
	rows := []string{""}
	for _, l := range c.d.Lines {
		rows = append(rows, "  "+styleText.Render(l))
	}
	if c.d.Command != "" {
		rows = append(rows, "")
		for i, l := range ui.Wrap(c.d.Command, w-18) {
			label := "              "
			if i == 0 {
				label = "  Выполнится:  "
			}
			rows = append(rows, styleDim.Render(label)+l)
		}
	}
	if c.d.Note != "" {
		rows = append(rows, "  "+styleDim.Render(c.d.Note))
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
	rows = append(rows, "", "  "+btnYes+"   "+btnNo, "")
	box := Pane(c.d.Title, "", rows, w)
	return overlayAt(base, box, max(0, (height-len(box))/2), (width-w)/2)
}

// overlayAt: строки box поверх base с этой строки и колонки; слева от окна - пусто.
func overlayAt(base, box []string, top, left int) []string {
	out := append([]string{}, base...)
	pad := strings.Repeat(" ", max(0, left))
	for i, l := range box {
		if top+i < len(out) {
			out[top+i] = pad + l
		}
	}
	return out
}

// ---------- шпаргалка ----------

type helpOverlay struct {
	title string
	ks    []Key
}

func (h *helpOverlay) update(msg tea.KeyPressMsg, app *App) tea.Cmd {
	app.overlay = nil
	return nil
}

func (h *helpOverlay) keys() []Key {
	return []Key{{"клавиша", "выполнить"}, {"esc", "закрыть"}}
}

// compose: ящик снизу над строкой клавиш - этот экран, список, везде; строка про раскладку.
func (h *helpOverlay) compose(base []string, width, height int) []string {
	w := width - 4
	cols := 3
	if width < 100 {
		cols = 2
	}
	colW := (w - 8) / cols
	screen, list := []Key{}, slices.Clone(listKeys)
	for _, k := range h.ks {
		switch k.K {
		case "/", "r", "space", "tab":
			list = append(list, k)
		default:
			screen = append(screen, k)
		}
	}
	columns := [][]string{keyColumn("ЭТОТ ЭКРАН", screen), keyColumn("СПИСОК", list)}
	if cols == 3 {
		columns = append(columns, keyColumn("ВЕЗДЕ", everyKeys))
	}
	n := 0
	for _, c := range columns {
		n = max(n, len(c))
	}
	rows := []string{""}
	for i := range n {
		line := "  "
		for _, c := range columns {
			cell := ""
			if i < len(c) {
				cell = c[i]
			}
			line += ui.Fit(cell, colW)
		}
		rows = append(rows, line)
	}
	rows = append(rows, "")
	if ru := layoutLine(screen); ru != "" {
		rows = append(rows, "  "+styleDim.Render(ui.Clip(ru, w-6)))
	}
	rows = append(rows, "  "+styleDim.Render("убрать - всегда в Корзину и с подтверждением; команда для терминала - внизу справа"), "")
	box := Pane("Клавиши: "+h.title, "", rows, w)
	return overlayAt(base, box, max(0, height-len(box)), 2)
}

func keyColumn(title string, keys []Key) []string {
	out := []string{styleHead.Render(title)}
	for _, k := range keys {
		out = append(out, styleKeyCap.Render(" "+k.K+" ")+" "+styleText.Render(k.D))
	}
	return out
}

// layoutLine: какие русские буквы работают на этом экране вместо латинских.
func layoutLine(keys []Key) string {
	back := map[rune]rune{}
	for ru, lat := range ruLayout {
		back[lat] = ru
	}
	var pairs []string
	for _, k := range keys {
		r := []rune(k.K)
		if len(r) != 1 || !unicode.IsLower(r[0]) {
			continue
		}
		if ru, ok := back[r[0]]; ok {
			pairs = append(pairs, string(ru)+" = "+k.K)
		}
	}
	if len(pairs) == 0 {
		return ""
	}
	return "русская раскладка работает позиционно: " + strings.Join(pairs, ", ")
}

// ---------- палитра ----------

type cmdline struct {
	input   textinput.Model
	list    List
	hits    []CommandEntry
	lastErr string
	// Группа экрана под палитрой: берётся один раз при открытии, а не на каждую букву.
	screenTitle string
	screen      []CommandEntry
}

// newCmdline: палитра ":"; text - что уже набрано, например пример для правки.
func newCmdline(app *App, text string) *cmdline {
	in := textinput.New()
	in.Prompt = ": "
	in.Placeholder = "команда tim - начни печатать, tab дополнит"
	in.SetWidth(60)
	st := in.Styles()
	st.Focused.Prompt = stylePrompt
	st.Focused.Placeholder = styleHint
	st.Cursor.Color = colAccent
	in.SetStyles(st)
	in.Focus()
	in.SetValue(text)
	in.CursorEnd()
	c := &cmdline{input: in, lastErr: app.lastErr}
	if p, ok := app.cur().(Paletter); ok {
		c.screenTitle, c.screen = p.Palette()
		for i := range c.screen {
			c.screen[i].Group = c.screenTitle
		}
	}
	c.refill(app)
	return c
}

// screenHits: группа экрана по запросу - каждое слово есть в имени, строке или описании, либо имя похоже.
func (c *cmdline) screenHits(q string) []CommandEntry {
	if q == "" || c.screenTitle == "" {
		return c.screen
	}
	words := strings.Fields(strings.ToLower(q))
	var out []CommandEntry
	for _, e := range c.screen {
		text := strings.ToLower(e.Name + " " + e.Line + " " + e.Desc)
		all := true
		for _, w := range words {
			all = all && strings.Contains(text, w)
		}
		if all || len(fuzzy.Filter(q, []string{e.Name})) > 0 {
			out = append(out, e)
		}
	}
	return out
}

func (c *cmdline) refill(app *App) {
	c.hits = nil
	q := strings.TrimSpace(c.input.Value())
	if q == "" && c.lastErr != "" {
		c.hits = append(c.hits, CommandEntry{Line: c.lastErr, Group: groupError})
	}
	c.hits = append(c.hits, c.screenHits(q)...)
	if app.opts.Commands != nil {
		all := app.opts.Commands(c.input.Value())
		if q != "" {
			names := make([]string, len(all))
			for i, e := range all {
				names[i] = e.Line
			}
			for _, m := range fuzzy.Filter(q, names) {
				c.hits = append(c.hits, all[m.Index])
			}
		} else {
			c.hits = append(c.hits, all...)
		}
	}
	// Группа экрана первой, за ней настоящие подстановки, шаблоны - последними.
	rank := map[string]int{groupError: 0, groupRecent: 2, groupSubst: 3, "": 4, groupCommands: 4, groupTemplate: 5}
	if c.screenTitle != "" {
		rank[c.screenTitle] = 1
	}
	for i := range c.hits {
		if c.hits[i].Template && c.hits[i].Group != c.screenTitle {
			c.hits[i].Group = groupTemplate
		}
	}
	slices.SortStableFunc(c.hits, func(a, b CommandEntry) int { return rank[a.Group] - rank[b.Group] })
	limit := 8
	if c.screenTitle != "" {
		limit = 12 // группа экрана целиком и пара общих команд под ней
	}
	if len(c.hits) > limit {
		c.hits = c.hits[:limit]
	}
	c.list.Rows = nil
	group := "-"
	for _, h := range c.hits {
		g := h.Group
		if g == "" {
			g = groupCommands
		}
		if g != group && g != groupError {
			group = g
			title := strings.ToUpper(g)
			if g == groupTemplate {
				title += " " + symSep + " enter подставит для правки"
			}
			if g == c.screenTitle {
				c.list.Rows = append(c.list.Rows, screenHeader(g))
			} else {
				c.list.Rows = append(c.list.Rows, headerRow(title))
			}
		}
		c.list.Rows = append(c.list.Rows, Row{Text: h.Line, Render: c.render(h)})
	}
	c.list.Cursor = 0
	c.list.Skip(1)
}

// screenHeader: заголовок группы экрана - имя заглавными, пояснение после точки серым.
func screenHeader(title string) Row {
	head, rest, _ := strings.Cut(title, " "+symSep+" ")
	return Row{Header: true, Text: title, Render: func(bool, int) string {
		line := "   " + styleHead.Render(strings.ToUpper(head))
		if rest != "" {
			line += styleDim.Render(" " + symSep + " " + rest)
		}
		return line
	}}
}

func (c *cmdline) render(h CommandEntry) func(bool, int) string {
	return func(sel bool, w int) string {
		if h.Group == groupError {
			line := styleBad.Render(ui.Clip(symCross+" последняя ошибка: "+h.Line, w-30))
			return SelRow(sel, w, line+"   "+styleDim.Render("enter - показать целиком"))
		}
		shown := h.Line
		if h.Name != "" {
			shown = h.Name
		}
		name := styleText.Render(ui.Fit(shown, 28))
		if sel {
			name = styleBold.Render(ui.Fit(shown, 28))
		}
		return SelRow(sel, w, name+styleDim.Render(ui.Clip(h.Desc, max(10, w-34))))
	}
}

func (c *cmdline) current() *CommandEntry {
	n := -1
	for i, r := range c.list.Rows {
		if !r.Header {
			n++
		}
		if i == c.list.Cursor && !r.Header && n < len(c.hits) {
			return &c.hits[n]
		}
	}
	return nil
}

func (c *cmdline) update(msg tea.KeyPressMsg, app *App) tea.Cmd {
	switch msg.String() {
	case "esc":
		if c.input.Value() != "" {
			c.input.SetValue("")
			c.refill(app)
			return nil
		}
		app.overlay = nil
		return nil
	case "up", "down":
		c.list.Key(msg.String(), 8)
		return nil
	case "tab":
		if hit := c.current(); hit != nil && hit.Group != groupError {
			c.input.SetValue(hit.Line)
			c.input.CursorEnd()
			c.refill(app)
		}
		return nil
	case "enter":
		return c.enter(app)
	}
	var cmd tea.Cmd
	before := c.input.Value()
	c.input, cmd = c.input.Update(msg)
	if c.input.Value() != before {
		c.refill(app)
	}
	return cmd
}

// enter выполняет выбранную подсказку; шаблон встаёт в строку для правки; похожего нет - набранное.
func (c *cmdline) enter(app *App) tea.Cmd {
	line := strings.TrimSpace(c.input.Value())
	if hit := c.current(); hit != nil {
		switch {
		case hit.Group == groupError:
			app.overlay = nil
			text := hit.Line
			return app.Push(NewResult("последняя ошибка", func() (string, error) { return text, nil }, ResultOpts{}))
		case hit.Template:
			c.input.SetValue(hit.Line)
			c.input.CursorEnd()
			c.refill(app)
			return nil
		case hit.Confirm != nil && app.opts.RunCommand != nil:
			d, run := *hit.Confirm, hit.Line
			yes := d.Yes
			d.Yes = func() tea.Cmd {
				if yes != nil {
					yes()
				}
				return app.Do(app.opts.RunCommand(run))
			}
			app.overlay = &confirmOverlay{d: d}
			return nil
		}
		line = hit.Line
	}
	app.overlay = nil
	if line == "" || app.opts.RunCommand == nil {
		return nil
	}
	return app.Do(app.opts.RunCommand(line))
}

func (c *cmdline) keys() []Key {
	return []Key{{"enter", "выполнить"}, {"tab", "дополнить"}, {"esc", "закрыть"}}
}

// compose: палитра сверху - ввод, подсказки, линия; экран ниже остаётся на своих местах серым.
func (c *cmdline) compose(base []string, width, height int) []string {
	top := []string{" " + c.input.View()}
	if len(c.hits) == 0 {
		top = append(top, "   "+styleDim.Render("ничего похожего "+symSep+" enter выполнит как набрано"))
	}
	top = append(top, c.list.View(width, len(c.list.Rows))...)
	top = append(top, "   "+styleLine.Render(strings.Repeat("\u2500", max(0, width-6))))
	out := append([]string{}, base...)
	for i, l := range top {
		if i < len(out) {
			out[i] = l
		}
	}
	return out
}
