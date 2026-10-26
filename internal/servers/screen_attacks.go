package servers

import (
	"strconv"
	"time"

	"github.com/TimurTurdyev/tim/internal/app"
	"github.com/TimurTurdyev/tim/internal/level"
	"github.com/TimurTurdyev/tim/internal/tui"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// AttacksScreen: кто ломится на сервер - находки по журналам, потом владельцы сетей.
func AttacksScreen(env *app.Env, target string, since time.Duration) tui.Screen {
	return tui.NewBoard(tui.BoardOpts{
		Crumbs:  []string{"Серверы", target, "кто ломится"},
		Loading: "читаю журналы " + target + " за " + window(since) + ", только чтение",
		Command: func(*tui.BoardRow) string { return "tim server " + target + " --attacks" },
		Load:    func(bool) tui.BoardData { return attacksData(target, since) },
		Palette: func() (string, []tui.CommandEntry) { return recipePalette(env, target) },
	})
}

func attacksData(target string, since time.Duration) tui.BoardData {
	var d tui.BoardData
	r, err := collectAttacks(target, since)
	if err != nil {
		d.Status = "не достучался"
		d.Head = []string{level.Lamp(level.Bad, target+": "+err.Error())}
		d.Empty = "отчёта нет"
		return d
	}
	d.Status = "за " + window(since)
	d.Head = []string{ui.Dim(sources(r.Attacks))}
	d.Columns = ui.Pad("ЧТО", 25) + "НАХОДКА"
	d.Wide = true
	for _, c := range r.Findings {
		row := findingRow(c)
		row.Group = "НАХОДКИ \u00b7 " + strconv.Itoa(len(r.Findings))
		d.Rows = append(d.Rows, row)
	}
	owners := r.Owners[:min(8, len(r.Owners))]
	for _, o := range owners {
		name := ownerName(o)
		d.Rows = append(d.Rows, tui.BoardRow{ID: "owner " + name, Group: "ОТКУДА БОЛЬШЕ ВСЕГО",
			Cells: ui.Fit(name, 60) + "  " + ui.Dim(attemptsText(o))})
	}
	d.Empty = "в журналах ничего тревожного"
	return d
}
