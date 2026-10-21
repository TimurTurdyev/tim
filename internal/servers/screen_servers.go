package servers

import (
	"fmt"
	"time"

	"github.com/TimurTurdyev/tim/internal/app"
	"github.com/TimurTurdyev/tim/internal/level"
	"github.com/TimurTurdyev/tim/internal/tui"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// ServersScreen: все серверы одной доской; enter открывает сервер, a - кто ломится, s - ssh.
func ServersScreen(env *app.Env, targets []Server) tui.Screen {
	return tui.NewBoard(tui.BoardOpts{
		Crumbs:  []string{"Серверы"},
		Loading: fmt.Sprintf("захожу по ssh на %d %s, только чтение", len(targets), ui.Plural(len(targets), "сервер", "сервера", "серверов")),
		Command: func(row *tui.BoardRow) string {
			if row == nil {
				return "tim servers"
			}
			return "tim server " + row.ID
		},
		Load: func(bool) tui.BoardData { return serversData(targets) },
		Actions: []tui.BoardAction{
			{Key: "enter", Label: "сервер", Do: func(row *tui.BoardRow) tui.Action { return tui.Action{Push: Screen(env, row.ID)} }},
			{Key: "a", Label: "кто ломится", Do: func(row *tui.BoardRow) tui.Action {
				return tui.Action{Push: AttacksScreen(row.ID, 24*time.Hour)}
			}},
			{Key: "s", Label: "ssh", Do: func(row *tui.BoardRow) tui.Action {
				return tui.Action{Exit: func() { env.After([]string{"ssh", row.ID}) }}
			}},
		},
	})
}

func serversData(targets []Server) tui.BoardData {
	states := collectAll(targets)
	d := tui.BoardData{Columns: serversHeader(), Empty: "серверов нет"}
	var bad int
	for _, s := range states {
		if s.level() >= level.Warn {
			bad++
		}
		d.Rows = append(d.Rows, serverRow(s))
	}
	d.Status = fmt.Sprintf("%d %s", len(states), ui.Plural(len(states), "сервер", "сервера", "серверов"))
	if bad > 0 {
		d.Status += fmt.Sprintf(", требуют внимания %d", bad)
	}
	return d
}

// serverRow: строка таблицы; в панели - все находки сервера с подсказками.
func serverRow(s serverState) tui.BoardRow {
	row := tui.BoardRow{ID: s.Target, Title: s.Target, Cells: serverLine(s)}
	if s.level() > level.OK {
		row.Sub = s.level().String()
	}
	if s.Err != "" {
		row.Pane = []string{s.Err, ui.Dim("маршрут и вход проверит tim ssh " + s.Target)}
		return row
	}
	row.Pane = []string{ui.Dim(summaryLine(s.Report))}
	for _, c := range s.Findings {
		row.Pane = append(row.Pane, ui.Pad(level.Lamp(c.Level, ui.Pad(c.Name, 12)), 16)+c.Detail)
		if c.Fix != "" && c.Level >= level.Warn {
			row.Pane = append(row.Pane, ui.Dim("  \u2192 "+c.Fix))
		}
	}
	if len(s.Findings) == 0 {
		row.Pane = append(row.Pane, "замечаний нет")
	}
	return row
}
