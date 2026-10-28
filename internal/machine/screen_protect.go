package machine

import (
	"path/filepath"
	"strconv"
	"strings"

	"github.com/TimurTurdyev/tim/internal/app"
	"github.com/TimurTurdyev/tim/internal/baseline"
	"github.com/TimurTurdyev/tim/internal/level"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/tui"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// protectTab: Машина › Защита - лампы сверху, новое в автозапуске, находки.
func protectTab(env *app.Env) tui.Tab {
	return tui.NewBoard(tui.BoardOpts{
		Crumbs: []string{"Машина"}, Tab: "Защита",
		Loading: "смотрю FileVault, файрвол, ключи ssh, автозапуск",
		Command: func(*tui.BoardRow) string { return "tim check --machine" },
		Load:    func(bool) tui.BoardData { return protectData(CheckProtection(env.Cfg)) },
		Actions: []tui.BoardAction{
			{Key: "space", Label: "отметить своим", Doing: "отмечаю своим",
				When: func(row *tui.BoardRow) bool { return strings.HasPrefix(row.ID, "launch ") },
				Run: func(row *tui.BoardRow) (string, error) {
					return "отмечено своим", baseline.Accept(MachineObject, "launch", strings.TrimPrefix(row.ID, "launch "))
				}},
			{Key: "o", Label: "Finder",
				When: func(row *tui.BoardRow) bool { return strings.HasPrefix(row.ID, "launch ") },
				Run: func(row *tui.BoardRow) (string, error) {
					file := strings.TrimPrefix(row.ID, "launch ")
					return "открыт " + filepath.Base(file), runner.Run("open", "-R", file)
				}},
		},
	}).(tui.Tab)
}


func protectData(p Protection) tui.BoardData {
	d := tui.BoardData{Tab: "Защита"}
	var lamps []string
	for _, l := range p.Lamps {
		lamps = append(lamps, level.Lamp(l.Level, l.Text))
	}
	if len(lamps) > 0 {
		d.Head = []string{strings.Join(lamps, "   ")}
	}
	d.Columns = ui.Pad("ЧТО", 25) + "НАХОДКА"
	d.Wide = true
	n := len(p.Findings) + len(p.New)
	if n > 0 {
		d.Status = ui.Count(n, "находка", "находки", "находок")
		if len(p.New) > 0 {
			d.Status += " · " + strconv.Itoa(len(p.New)) + " новое"
		}
	}
	for _, it := range p.New {
		d.Rows = append(d.Rows, tui.BoardRow{ID: "launch " + it.Key, Group: "НОВОЕ · " + strconv.Itoa(len(p.New)),
			Cells:  ui.Pad(level.Lamp(level.Warn, "автозапуск"), 25) + it.Text,
			Inline: "что делать: не ставил - выгрузи launchctl bootout и убери файл в Корзину, проверь, что он запускает"})
	}
	for _, f := range p.Findings {
		d.Rows = append(d.Rows, tui.BoardRow{ID: f.Name, Group: "ВНИМАНИЕ · " + strconv.Itoa(len(p.Findings)),
			Cells:  ui.Pad(level.Lamp(f.Level, f.Name), 25) + f.Detail,
			Inline: "что делать: " + f.Fix})
	}
	d.Empty = "замечаний нет"
	return d
}
