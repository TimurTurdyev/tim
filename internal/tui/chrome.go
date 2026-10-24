package tui

import (
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/TimurTurdyev/tim/internal/brand"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// Каркас экрана: верхняя строка с крошками и статусом, тело, строка состояния, нижняя строка.
// Нижняя строка: слоты в постоянном порядке и три веса. Главное действие - тёмным на бирюзовом,
// клавиши экрана - на подложке, общие (: ? esc) - серым без подложки и всегда последними.
// Справа - то же самое командой для терминала.

// dimmed: экран рисуется под окном - маскот лок-апа на это время не рисуется, место остаётся.
var dimmed bool

// keySlot: место клавиши в строке - enter, /, буквы экрана, space, tab, r.
func keySlot(k string) int {
	switch k {
	case "enter":
		return 0
	case "/":
		return 1
	case "space":
		return 3
	case "tab", "shift+tab":
		return 4
	case "r":
		return 5
	}
	return 2
}

func quietKey(k string) bool { return k == "?" || k == "esc" || k == ":" }

// screenKeys: клавиши экрана по слотам и общие в конце. стрелки не пишутся: их знают все.
func (a *App) screenKeys(s Screen) []Key {
	esc := "назад"
	if len(a.stack) == 1 {
		esc = "выход"
	}
	var keys []Key
	for _, k := range s.Keys() {
		switch k.K {
		case "esc":
			esc = k.D
		case symUpDown, "?", ":", "q":
		default:
			keys = append(keys, k)
		}
	}
	slices.SortStableFunc(keys, func(x, y Key) int { return keySlot(x.K) - keySlot(y.K) })
	if !a.typing() {
		if len(a.stack) == 1 && a.opts.RunCommand != nil && a.width >= 100 {
			keys = append(keys, Key{":", "команда"})
		}
		keys = append(keys, Key{"?", "клавиши"})
	}
	return append(keys, Key{"esc", esc})
}

func (a *App) keyBar(s Screen) string {
	keys := a.screenKeys(s)
	cmd := s.Command()
	_, inlined := a.overlay.(inline)
	switch _, confirm := a.overlay.(*confirmOverlay); {
	case a.overlay != nil && !inlined:
		keys = a.overlay.keys()
		if !confirm {
			cmd = ""
		}
	case a.busy():
		keys = []Key{{"esc", "назад"}}
	}
	right := ""
	if cmd != "" {
		right = styleKeyDesc.Render("$ "+cmd) + " "
	}
	return fitKeys(keys, right, a.width)
}

func keyChip(k Key) string {
	switch {
	case k.K == "enter":
		return styleKeyMain.Render(" "+k.K+" ") + styleKeyMainDesc.Render(" "+k.D)
	case quietKey(k.K):
		return styleKeyQuiet.Render(" " + k.K + "  " + k.D)
	}
	return styleKeyCap.Render(" "+k.K+" ") + styleKeyDesc.Render(" "+k.D)
}

func keyLine(keys []Key) string {
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = keyChip(k)
	}
	return strings.Join(parts, "  ")
}

// fitKeys: строка клавиш шириной width. Если не влезает, с конца уходят клавиши экрана; первая
// клавиша экрана держится, пока рядом помещается команда, дальше уходит команда. Общие не уходят.
func fitKeys(keys []Key, right string, width int) string {
	fits := func(ks []Key, r string) bool {
		gap := 0
		if r != "" {
			gap = 3
		}
		return ui.Width(keyLine(ks))+gap+ui.Width(r) <= width
	}
	droppable := func(ks []Key) int {
		for i := len(ks) - 1; i > 0; i-- {
			if !quietKey(ks[i].K) {
				return i
			}
		}
		return -1
	}
	ks := slices.Clone(keys)
	keep := 0
	if len(ks) > 0 && ks[0].K == "enter" {
		keep = 1
	}
	for !fits(ks, right) {
		i := droppable(ks)
		if right != "" && i <= keep {
			right = ""
			continue
		}
		if i < 0 {
			break
		}
		ks = slices.Delete(ks, i, i+1)
	}
	left := ui.Clip(keyLine(ks), width)
	return left + strings.Repeat(" ", max(0, width-ui.Width(left)-ui.Width(right))) + right
}

func (a *App) View() tea.View {
	if a.quit {
		return tea.NewView("")
	}
	w, h := a.width, a.height
	s := a.cur()
	var rows []string
	if len(s.Crumbs()) > 0 {
		rows = append(rows, a.topBar(s))
	}
	// Тело - всё между верхней и нижней строками; его последняя строка - строка состояния.
	viewH := max(1, h-len(rows)-2)
	_, inlined := a.overlay.(inline)
	over := a.overlay != nil && !inlined
	lines := strings.Split(viewDimmed(s, w, viewH, over), "\n")
	if len(lines) > viewH {
		lines = lines[:viewH]
	}
	for len(lines) < viewH {
		lines = append(lines, "")
	}
	if over {
		// Экран под окном остаётся на местах, но серым: видно, к чему относится решение.
		for i, l := range lines {
			lines[i] = styleDim.Render(ui.StripANSI(l))
		}
		lines = a.overlay.compose(lines, w, viewH)
	}
	// Терминал режет длинную строку молча; тильда хотя бы показывает, что там было ещё.
	for i, l := range lines {
		lines[i] = ui.Clip(l, w)
	}
	rows = append(rows, lines...)
	status := a.statusLine(w)
	if in, ok := a.overlay.(inline); ok {
		status = in.line(w)
	}
	rows = append(rows, status, a.keyBar(s))
	v := tea.NewView(strings.Join(rows, "\n"))
	v.AltScreen = true
	if c, ok := s.(cursored); ok && a.overlay == nil {
		if cur := c.Cursor(); cur != nil {
			if len(s.Crumbs()) > 0 {
				cur.Y++
			}
			v.Cursor = cur
		}
	}
	return v
}

// topBar: чип tim и крошки слева, статус справа. Родители серые, текущий экран жирным.
func (a *App) topBar(s Screen) string {
	crumbs := s.Crumbs()
	left := styleBrand.Render(" " + brand.Name + " ")
	for i, c := range crumbs {
		st := styleCrumb
		if i == len(crumbs)-1 {
			st = styleCrumbNow
		}
		left += styleCrumb.Render(" "+symPointer+" ") + st.Render(c)
	}
	left = ui.Clip(left, a.width)
	// Крошки важнее статуса: на узком экране он обрезается или уходит.
	right := ""
	if room := a.width - ui.Width(left) - 4; room >= 8 {
		right = statusText(ui.Clip(s.Status(), room)) + " "
	}
	return left + strings.Repeat(" ", max(0, a.width-ui.Width(left)-ui.Width(right))) + right
}

// statusText красит статус по первому знаку; знак работы становится текущим кадром.
func statusText(s string) string {
	switch {
	case strings.HasPrefix(s, spinFrames[0]):
		return styleStatus.Render(Spinner() + strings.TrimPrefix(s, spinFrames[0]))
	case strings.HasPrefix(s, symCheck):
		return styleGood.Render(s)
	case strings.HasPrefix(s, symCross):
		return styleBad.Render(s)
	case strings.HasPrefix(s, symWarn):
		return styleAmber.Render(s)
	}
	return styleStatus.Render(s)
}

// screenTitle: как назвать экран в шпаргалке.
func screenTitle(s Screen) string {
	if c := s.Crumbs(); len(c) > 0 {
		return c[len(c)-1]
	}
	return "главный"
}

// viewDimmed: экран целиком, под окном - без маскота.
func viewDimmed(s Screen, w, h int, on bool) string {
	dimmed = on
	defer func() { dimmed = false }()
	return s.View(w, h)
}
