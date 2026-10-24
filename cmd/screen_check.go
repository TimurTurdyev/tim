package cmd

import (
	"fmt"
	"strings"
	"sync"

	"github.com/TimurTurdyev/tim/internal/health"
	"github.com/TimurTurdyev/tim/internal/sites"
	"github.com/TimurTurdyev/tim/internal/tui"
	"github.com/TimurTurdyev/tim/internal/ui"
)

func lamp(level health.Level, text string) string {
	switch level {
	case health.Bad:
		return ui.Bad("\u2715 " + text)
	case health.Warn:
		return ui.Warn("\u25b2 " + text)
	case health.Skip:
		return ui.Dim("- " + text)
	}
	return ui.OK("\u25cf " + text)
}

// ---------- проверка проектов ----------

func checkScreen() tui.Screen {
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
				rows = checkAll()
			}
			return checkData(rows, only)
		},
		Actions: []tui.BoardAction{
			{Key: "enter", Label: "подробно", Do: func(row *tui.BoardRow) tui.Action {
				if row == nil {
					return tui.Action{}
				}
				return resultAction([]string{"check", row.ID})
			}},
			{Key: "e", Label: "только проблемы", Run: func(*tui.BoardRow) (string, error) {
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
func checkAll() []health.Row {
	projects, err := checkProjects(nil)
	if err != nil {
		return nil
	}
	rows := make([]health.Row, len(projects))
	sem := make(chan struct{}, 8)
	var wg sync.WaitGroup
	for i, p := range projects {
		wg.Go(func() {
			sem <- struct{}{}
			defer func() { <-sem }()
			rows[i] = sites.CheckRow(p, cfg.ProjectDir(p), checkLocal, checkNet)
		})
	}
	wg.Wait()
	kept := rows[:0]
	for _, r := range rows {
		if r.Level() != health.Skip {
			kept = append(kept, r)
		}
	}
	health.Sort(kept)
	return kept
}

func checkData(rows []health.Row, only bool) tui.BoardData {
	var d tui.BoardData
	ok, warn, bad := health.Counts(rows)
	d.Status = fmt.Sprintf("%d %s", len(rows), ui.Plural(len(rows), "проект", "проекта", "проектов"))
	if warn+bad > 0 {
		d.Status += fmt.Sprintf(" \u00b7 %d требуют внимания", warn+bad)
	} else {
		d.Status += " \u00b7 всё в порядке"
	}
	cols := checkCols[:0:0]
	for _, col := range checkCols {
		for _, r := range rows {
			if r.Get(col.name).Level != health.Skip {
				cols = append(cols, col)
				break
			}
		}
	}
	head := ui.Pad("ПРОЕКТ", 20)
	for _, col := range cols {
		head += ui.Pad(strings.ToUpper(col.name), col.width)
	}
	d.Head = []string{ui.Dim(head)}
	hidden := 0
	for _, r := range rows {
		if only && r.Level() < health.Warn {
			hidden++
			continue
		}
		row := tui.BoardRow{ID: r.Name, Title: r.Name}
		if p, err := cfg.Pick(r.Name); err == nil && p.URL != "" {
			row.Sub = p.URL
		}
		row.Cells = ui.Pad(r.Name, 20)
		for _, col := range cols {
			c := r.Get(col.name)
			row.Cells += ui.Pad(lamp(c.Level, ui.Clip(c.Text, col.width-3)), col.width)
		}
		for _, c := range r.Problems() {
			text := c.Detail
			if text == "" {
				text = c.Text
			}
			row.Pane = append(row.Pane, lamp(c.Level, c.Name+": "+text))
		}
		if len(row.Pane) == 0 {
			row.Pane = []string{ui.OK("\u25cf всё в порядке")}
		}
		row.Pane = append(row.Pane, ui.Dim("\u2192 enter покажет проверку целиком, tim domain "+row.Sub+" - домен"))
		d.Rows = append(d.Rows, row)
	}
	if hidden > 0 {
		d.Foot = append(d.Foot, ui.Dim(fmt.Sprintf("ещё %d в порядке, e - показать", hidden)))
	}
	d.Foot = append(d.Foot, checkMachine()...)
	d.Empty = fmt.Sprintf("проблем нет: ок %d", ok)
	if len(rows) == 0 {
		d.Empty = "проверять нечего: ни у одного проекта нет git и адреса сайта"
	}
	return d
}
