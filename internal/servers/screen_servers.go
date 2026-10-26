package servers

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/TimurTurdyev/tim/internal/app"
	"github.com/TimurTurdyev/tim/internal/level"
	"github.com/TimurTurdyev/tim/internal/tui"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// ServersScreen: серверы из кэша последнего состояния - без входа по ssh на каждый; enter - экран
// сервера со сбором по ssh, r - собрать выбранный заново.
func ServersScreen(env *app.Env, targets []Server) tui.Screen {
	return tui.NewBoard(tui.BoardOpts{
		Crumbs:  []string{"Серверы"},
		Static:  true,
		Search:  true,
		Loading: "смотрю, что известно о серверах",
		Command: func(row *tui.BoardRow) string {
			if row == nil {
				return "tim servers"
			}
			return "tim server " + row.ID
		},
		Load:  func(bool) tui.BoardData { return serversData(targets) },
		Enter: func(*tui.BoardRow) string { return "состояние по ssh, tim остаётся" },
		Actions: []tui.BoardAction{
			{Key: "enter", Label: "состояние", Do: func(row *tui.BoardRow) tui.Action { return tui.Action{Push: Screen(env, row.ID)} }},
			{Key: "s", Label: "ssh", Note: "закроет tim", Do: func(row *tui.BoardRow) tui.Action {
				return tui.Action{Exit: func() { env.After([]string{"ssh", row.ID}) }}
			}},
			{Key: "a", Label: "кто ломится", Do: func(row *tui.BoardRow) tui.Action {
				return tui.Action{Push: AttacksScreen(env, row.ID, 24*time.Hour)}
			}},
			{Key: "r", Label: "обновить", Doing: "захожу по ssh", Free: true, Run: func(row *tui.BoardRow) (string, error) {
				if row == nil {
					return "", nil
				}
				s := collect(Server{Alias: row.ID})
				if s.Err != "" {
					return "", ui.Errorf("%s: %s", row.ID, s.Err)
				}
				return row.ID + ": " + s.state(), nil
			}},
		},
	})
}

func serversData(targets []Server) tui.BoardData {
	nameW := len("СЕРВЕР")
	for _, t := range targets {
		nameW = max(nameW, ui.Width(t.Alias))
	}
	nameW += 2
	d := tui.BoardData{Columns: ui.Pad("СЕРВЕР", nameW) + ui.Pad("ОС", 20) + ui.Pad("РАБОТАЕТ", 10) + "НАХОДКИ"}
	d.Empty = "Серверов нет: tim знает их из ~/.ssh/config и поля ssh у проектов"
	var oldest time.Time
	for _, t := range targets {
		s, at, ok := saved(t.Alias)
		if ok && (oldest.IsZero() || at.Before(oldest)) {
			oldest = at
		}
		d.Rows = append(d.Rows, serverRow(t, s, at, ok, nameW))
	}
	d.Status = ui.Count(len(targets), "сервер", "сервера", "серверов")
	if !oldest.IsZero() {
		d.Status += " \u00b7 проверено " + ago(oldest)
	}
	return d
}

func ago(t time.Time) string {
	if time.Since(t) < time.Minute {
		return "только что"
	}
	return ui.Short(time.Since(t))
}

// serverRow: строка из кэша; в карточке - находки, диск и память, когда снято.
func serverRow(t Server, s serverState, at time.Time, ok bool, nameW int) tui.BoardRow {
	row := tui.BoardRow{ID: t.Alias, Find: strings.Join(t.Projects, " ")}
	card := &tui.Card{Title: t.Alias, Sub: "ssh " + t.Alias}
	row.Card = card
	if !ok {
		row.Cells = ui.Pad(t.Alias, nameW) + ui.Dim("- не проверялся")
		card.Path = "не проверялся: enter соберёт состояние по ssh"
		return row
	}
	r := s.Report
	warn := 0
	var names []string
	for _, c := range s.Findings {
		if c.Level >= level.Warn {
			warn++
			names = append(names, c.Name)
		}
	}
	finds := ""
	if warn > 0 {
		finds = level.Lamp(s.level(), strconv.Itoa(warn))
	}
	row.Cells = ui.Pad(t.Alias, nameW) + ui.Fit(r.OS, 20) + ui.Pad(ui.Short(r.Uptime), 10) + finds
	card.Path = "проверялся " + ago(at)
	if len(names) > 0 {
		card.Facts = append(card.Facts, tui.Fact{Label: "находки", Value: level.Lamp(level.Warn, strings.Join(names, ", "))})
	}
	var load []string
	if disk, ok := r.Root(); ok {
		load = append(load, fmt.Sprintf("диск %d%%", disk.Pct))
	}
	if r.MemTotal > 0 {
		load = append(load, fmt.Sprintf("память %d%%", (r.MemTotal-r.MemAvail)*100/r.MemTotal))
	}
	if len(load) > 0 {
		card.Facts = append(card.Facts, tui.Fact{Label: "занято", Value: strings.Join(load, " \u00b7 ")})
	}
	if len(t.Projects) > 0 {
		card.Facts = append(card.Facts, tui.Fact{Label: "проекты", Value: strings.Join(t.Projects, ", ")})
	}
	return row
}
