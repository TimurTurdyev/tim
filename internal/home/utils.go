package home

import (
	"strings"

	"github.com/TimurTurdyev/tim/internal/tui"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// utilsScreen: мелкие утилиты списком; enter набирает утилиту в строке ":" - почти всем нужен аргумент.
func (h *Home) utilsScreen() tui.Screen {
	var data tui.BoardData
	data.Status = ui.Count(len(h.utils), "утилита", "утилиты", "утилит")
	nameW := 0
	for _, n := range h.utils {
		nameW = max(nameW, len(n))
	}
	for _, n := range h.utils {
		c, _, err := h.root.Find([]string{n})
		if err != nil || c == h.root {
			continue
		}
		row := tui.BoardRow{ID: n, Title: "tim " + n, Sub: c.Short, Cells: ui.Label(ui.Pad(n, nameW+2)) + c.Short}
		for _, l := range strings.Split(h.examples["tim "+n], "\n") {
			if l = strings.TrimSpace(l); l != "" {
				row.Pane = append(row.Pane, l)
			}
		}
		row.Pane = append(row.Pane, ui.Dim("→ enter - набрать в строке \":\""))
		data.Rows = append(data.Rows, row)
	}
	return tui.NewBoard(tui.BoardOpts{
		Crumbs: []string{"Утилиты"},
		Static: true,
		Command: func(row *tui.BoardRow) string {
			if row == nil {
				return ""
			}
			return "tim " + row.ID + " --help"
		},
		Load: func(bool) tui.BoardData { return data },
		Actions: []tui.BoardAction{
			{Key: "enter", Label: "набрать", Do: func(row *tui.BoardRow) tui.Action {
				return tui.Action{Command: row.ID + " "}
			}},
		},
	})
}
