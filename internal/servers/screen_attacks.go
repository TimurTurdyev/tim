package servers

import (
	"time"

	"github.com/TimurTurdyev/tim/internal/level"
	"github.com/TimurTurdyev/tim/internal/tui"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// AttacksScreen: кто ломится на сервер - находки по журналам, потом владельцы сетей.
func AttacksScreen(target string, since time.Duration) tui.Screen {
	return tui.NewBoard(tui.BoardOpts{
		Crumbs:  []string{"Сервер", target, "кто ломится"},
		Loading: "читаю журналы " + target + " за " + window(since) + ", только чтение",
		Command: func(*tui.BoardRow) string { return "tim server " + target + " --attacks" },
		Load:    func(bool) tui.BoardData { return attacksData(target, since) },
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
	for _, c := range r.Findings {
		d.Rows = append(d.Rows, findingRow(c))
	}
	for _, o := range r.Owners[:min(8, len(r.Owners))] {
		name := ownerName(o)
		d.Rows = append(d.Rows, tui.BoardRow{ID: "owner " + name, Title: name,
			Cells: ui.Pad("  сеть", 16) + ui.Pad(ui.Clip(name, 44), 46) + ui.Dim(attemptsText(o)),
			Pane:  []string{name, attemptsText(o)}})
	}
	d.Empty = "в журналах ничего тревожного"
	return d
}
