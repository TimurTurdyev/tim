package sites

import (
	"fmt"
	"strings"
	"sync"

	"github.com/TimurTurdyev/tim/internal/app"
	"github.com/TimurTurdyev/tim/internal/config"
	"github.com/TimurTurdyev/tim/internal/health"
	"github.com/TimurTurdyev/tim/internal/level"
	"github.com/TimurTurdyev/tim/internal/tui"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// CheckScreen: проверка проектов, как у tim check без флагов.
func CheckScreen(env *app.Env) tui.Screen { return checkScreen(env, checkOptions{hours: 8}) }

// checkScreen: таблица проектов, панель с проблемами, только проблемные по e.
func checkScreen(env *app.Env, o checkOptions) tui.Screen {
	var (
		mu   sync.Mutex
		rows []health.Row
		only bool
	)
	return tui.NewBoard(tui.BoardOpts{
		Crumbs:  []string{"Проекты", "Проверка"},
		Loading: "обхожу проекты: git, сайт, сертификат, почта, домен - несколько секунд",
		Command: func(row *tui.BoardRow) string {
			if row != nil {
				return "tim check " + row.ID
			}
			return "tim check"
		},
		Load: func(fresh bool) tui.BoardData {
			mu.Lock()
			defer mu.Unlock()
			if rows == nil || fresh {
				rows = checkAll(env.Cfg, o)
			}
			return checkData(env.Cfg, rows, only, o.hours)
		},
		Actions: []tui.BoardAction{
			{Key: "enter", Label: "подробно", Do: func(row *tui.BoardRow) tui.Action {
				if row == nil {
					return tui.Action{}
				}
				return env.Result([]string{"check", row.ID})
			}},
			{Key: "e", Label: "только проблемы", Free: true, Run: func(*tui.BoardRow) (string, error) {
				mu.Lock()
				only = !only
				mu.Unlock()
				if only {
					return "только проблемные; e - все", nil
				}
				return "все проекты", nil
			}},
		},
	})
}

// checkAll: все проекты реестра, как tim check без аргументов.
func checkAll(cfg *config.Config, o checkOptions) []health.Row {
	projects, err := projectsOrDomains(cfg, nil)
	if err != nil {
		return nil
	}
	return checkRows(cfg, projects, o)
}

func checkData(cfg *config.Config, rows []health.Row, only bool, hours float64) tui.BoardData {
	var d tui.BoardData
	ok, warn, bad := health.Counts(rows)
	d.Status = ui.Count(len(rows), "проект", "проекта", "проектов")
	if warn+bad > 0 {
		d.Status += fmt.Sprintf(" \u00b7 %d требуют внимания", warn+bad)
	} else {
		d.Status += " \u00b7 всё в порядке"
	}
	cols := usedCols(rows)
	nameW := nameWidth(rows)
	head := ui.Pad("ПРОЕКТ", nameW+1)
	for _, col := range cols {
		head += ui.Pad(strings.ToUpper(col.name), col.width)
	}
	d.Columns = ui.Dim(head)
	hidden := 0
	for _, r := range rows {
		if only && r.Level() < level.Warn {
			hidden++
			continue
		}
		row := tui.BoardRow{ID: r.Name, Title: r.Name}
		if p, err := cfg.Pick(r.Name); err == nil && p.URL != "" {
			row.Sub = p.URL
		}
		row.Cells = ui.Fit(r.Name, nameW) + " "
		for _, col := range cols {
			c := r.Get(col.name)
			row.Cells += ui.Pad(level.Lamp(c.Level, ui.Clip(c.Text, col.width-3)), col.width)
		}
		row.Pane = problemLines(r)
		hint := "\u2192 enter покажет проверку целиком"
		if row.Sub != "" {
			hint += ", tim domain " + row.Sub + " - домен"
		}
		row.Pane = append(row.Pane, ui.Dim(hint))
		d.Rows = append(d.Rows, row)
	}
	if hidden > 0 {
		d.Foot = append(d.Foot, ui.Dim(fmt.Sprintf("ещё %d в порядке, e - показать", hidden)))
	}
	d.Foot = append(d.Foot, machineNotes(cfg, hours)...)
	d.Empty = fmt.Sprintf("проблем нет: ок %d", ok)
	if len(rows) == 0 {
		d.Empty = "проверять нечего: ни у одного проекта нет git и адреса сайта"
	}
	return d
}
