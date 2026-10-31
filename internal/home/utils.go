package home

import (
	"strconv"
	"strings"

	"github.com/TimurTurdyev/tim/internal/tui"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// runsBare: утилиты, которым аргумент не нужен - enter выполняет их сразу, а не спрашивает строкой.
var runsBare = map[string]bool{"uuid": true, "ts": true, "pass": true}

// askHints: что вводить, если вызова из справки мало.
var askHints = map[string]string{"cron": "минута час день месяц день-недели; пусто - как писать"}

// utilsScreen: мелкие утилиты списком; карточка - их справка из того же места, что --help.
func (h *Home) utilsScreen() tui.Screen {
	var data tui.BoardData
	data.Status = ui.Count(len(h.utils), "утилита", "утилиты", "утилит")
	nameW := ui.Width("УТИЛИТА")
	for _, n := range h.utils {
		nameW = max(nameW, ui.Width(n))
	}
	data.Columns = ui.Pad("УТИЛИТА", nameW+3) + "ЧТО ДЕЛАЕТ"
	for _, n := range h.utils {
		c, _, err := h.root.Find([]string{n})
		if err != nil || c == h.root {
			continue
		}
		row := tui.BoardRow{ID: n, Find: c.Short, Cells: ui.Pad(n, nameW+3) + ui.Dim(c.Short)}
		data.Wide = data.Wide || nameW+3+ui.Width(c.Short)+3 > 56
		card := &tui.Card{Title: n, Enter: "ввести, что проверить, - строкой внизу"}
		if runsBare[n] {
			card.Enter = "выполнить сразу"
		}
		if long, _, _ := strings.Cut(strings.TrimSpace(c.Long), "\n"); long != "" {
			card.About = append(card.About, long)
		}
		card.Facts = append(card.Facts, tui.Fact{Label: "вызов", Value: c.Use})
		for i, ex := range examplesOf(h.exampleText(n)) {
			if i == 3 {
				break
			}
			card.Extra = append(card.Extra, tui.KeyCap(strconv.Itoa(i+1))+" "+ex)
		}
		row.Card = card
		data.Rows = append(data.Rows, row)
	}
	return tui.NewBoard(tui.BoardOpts{
		Crumbs: []string{"Утилиты"},
		Static: true,
		Search: true,
		Command: func(row *tui.BoardRow) string {
			if row == nil {
				return ""
			}
			return "tim " + row.ID + " --help"
		},
		Load: func(bool) tui.BoardData { return data },
		Actions: append([]tui.BoardAction{
			{Key: "enter", Label: "ввести", Do: func(row *tui.BoardRow) tui.Action {
				if runsBare[row.ID] {
					return h.result([]string{row.ID})
				}
				return h.askUtil(row.ID)
			}},
			{Key: "1-3", Label: "дальше", Free: true},
		}, h.exampleKeys()...),
	})
}

// askUtil: строка ввода внизу с примером в подсказке; пустой enter - утилита без аргумента (cron
// тогда объясняет, как писать выражение).
func (h *Home) askUtil(name string) tui.Action {
	placeholder, hint := "", ""
	if ex := examplesOf(h.exampleText(name)); len(ex) > 0 {
		placeholder = strings.Trim(strings.TrimSpace(strings.TrimPrefix(ex[0], name)), `"`)
	}
	if c, _, err := h.root.Find([]string{name}); err == nil {
		hint = "tim " + c.Use
	}
	if hh, ok := askHints[name]; ok {
		hint = hh
	}
	return tui.Action{Ask: &tui.Ask{Placeholder: placeholder, Hint: hint, Done: func(text string) tui.Action {
		return h.result(append([]string{name}, splitArgs(text)...))
	}}}
}

// splitArgs: строка как в оболочке - кавычки держат пробелы внутри и снимаются.
func splitArgs(s string) []string {
	var out []string
	var cur strings.Builder
	quote, has := rune(0), false
	for _, r := range s {
		switch {
		case quote != 0 && r == quote:
			quote = 0
		case quote == 0 && (r == '"' || r == '\''):
			quote, has = r, true
		case quote == 0 && (r == ' ' || r == '\t'):
			if cur.Len() > 0 || has {
				out = append(out, cur.String())
				cur.Reset()
				has = false
			}
		default:
			cur.WriteRune(r)
		}
	}
	if cur.Len() > 0 || has {
		out = append(out, cur.String())
	}
	return out
}

// exampleKeys: цифры 1-3 ставят пример выбранной утилиты в ":" для правки.
func (h *Home) exampleKeys() []tui.BoardAction {
	var out []tui.BoardAction
	for i := range 3 {
		out = append(out, tui.BoardAction{Key: strconv.Itoa(i + 1), Label: "дальше", Hidden: true, Do: func(row *tui.BoardRow) tui.Action {
			c, _, err := h.root.Find([]string{row.ID})
			if err != nil {
				return tui.Action{}
			}
			if ex := examplesOf(h.exampleText(c.Name())); i < len(ex) {
				return tui.Action{Command: ex[i]}
			}
			return tui.Action{}
		}})
	}
	return out
}

// exampleText: примеры утилиты из справки.
func (h *Home) exampleText(name string) string {
	if ex := h.examples["tim "+name]; ex != "" {
		return ex
	}
	if c, _, err := h.root.Find([]string{name}); err == nil {
		return c.Example
	}
	return ""
}

// examplesOf: примеры справки без "tim " и комментариев - готовые строки для ":". Строки с трубой
// в ":" не выполнить - их нет.
func examplesOf(example string) []string {
	var out []string
	for _, l := range strings.Split(example, "\n") {
		l = strings.TrimSpace(l)
		if !strings.HasPrefix(l, "tim ") {
			continue
		}
		l, _, _ = strings.Cut(l, "  #")
		out = append(out, strings.TrimSpace(strings.TrimPrefix(l, "tim ")))
	}
	return out
}
