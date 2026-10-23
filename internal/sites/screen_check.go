package sites

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/TimurTurdyev/tim/internal/app"
	"github.com/TimurTurdyev/tim/internal/config"
	"github.com/TimurTurdyev/tim/internal/health"
	"github.com/TimurTurdyev/tim/internal/level"
	"github.com/TimurTurdyev/tim/internal/machine"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/tui"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// CheckScreen: проверка проектов, как у tim check без флагов.
func CheckScreen(env *app.Env) tui.Screen {
	return checkScreen(env, checkOptions{older: machine.ForgottenAfter}, true)
}

// CheckTab: вкладка Проверка экрана проектов - из кэша проверок, r проверяет заново.
func CheckTab(env *app.Env) tui.Tab {
	return checkScreen(env, checkOptions{older: machine.ForgottenAfter}, false).(tui.Tab)
}

// checkScreen: таблица проектов с группами ПРОБЛЕМЫ и В ПОРЯДКЕ, карточка с тем, что не так и что
// делать. Вкладка открывается из кэша, сеть - по r; tim check в терминале сразу проверяет заново.
func checkScreen(env *app.Env, o checkOptions, now bool) tui.Screen {
	var (
		mu   sync.Mutex
		rows []health.Row
		at   time.Time
	)
	return tui.NewBoard(tui.BoardOpts{
		Crumbs:  []string{"Проекты"},
		Tab:     "Проверка",
		Loading: loadingText(o),
		Search:  true,
		Command: func(row *tui.BoardRow) string {
			if row != nil {
				return "tim check " + row.ID
			}
			return "tim check"
		},
		Load: func(fresh bool) tui.BoardData {
			mu.Lock()
			defer mu.Unlock()
			if rows == nil && !fresh && !now {
				rows, at = savedRows(env.Cfg)
			}
			if fresh || rows == nil && now {
				rows, at = checkAll(env.Cfg, o), time.Now()
			}
			return checkData(env.Cfg, rows, at)
		},
		Enter: func(row *tui.BoardRow) string {
			return "подробно: check " + row.ID + ", все проверки и причины"
		},
		Actions: []tui.BoardAction{
			{Key: "enter", Label: "подробно", Do: func(row *tui.BoardRow) tui.Action {
				return env.Result([]string{"check", row.ID})
			}},
			{Key: "c", Label: "проверить", Doing: "проверяю", Run: func(row *tui.BoardRow) (string, error) {
				p, err := env.Cfg.Pick(row.ID)
				if err != nil {
					return "", err
				}
				r := checkRows(env.Cfg, []*config.Project{p}, o)
				if len(r) == 0 {
					return row.ID + ": проверять нечего", nil
				}
				mu.Lock()
				for i := range rows {
					if rows[i].Name == r[0].Name {
						rows[i] = r[0]
					}
				}
				mu.Unlock()
				return row.ID + ": " + verdict(r[0]), nil
			}},
			{Key: "w", Label: "браузер", Doing: "открываю сайт", Run: func(row *tui.BoardRow) (string, error) {
				p, err := env.Cfg.Pick(row.ID)
				if err != nil || p.URL == "" {
					return "", ui.Errorf("у %s нет адреса сайта", row.ID)
				}
				return row.ID + ": сайт открыт в браузере", runner.OpenBrowser(p.URL)
			}},
		},
	})
}

// savedRows: проверки проектов реестра из кэша и время самой старой из них.
func savedRows(cfg *config.Config) ([]health.Row, time.Time) {
	var rows []health.Row
	var oldest time.Time
	for _, s := range health.SavedRows() {
		if _, ok := cfg.Project(s.Name); !ok {
			continue
		}
		rows = append(rows, s.Row)
		if oldest.IsZero() || s.At.Before(oldest) {
			oldest = s.At
		}
	}
	health.Sort(rows)
	return rows, oldest
}

// verdict: исход проверки одного проекта для строки состояния.
func verdict(r health.Row) string {
	switch r.Level() {
	case level.Bad, level.Warn:
		return ui.Count(len(r.Problems()), "проблема", "проблемы", "проблем")
	}
	return "в порядке"
}

// loadingText: что проверяется; с серверами обход заметно дольше.
func loadingText(o checkOptions) string {
	if o.servers {
		return "обхожу проекты и их серверы по ssh: git, сайт, сертификат, почта, домен, сервер - около минуты"
	}
	return "обхожу проекты: git, сайт, сертификат, почта, домен - несколько секунд"
}

// checkAll: все проекты реестра, как tim check без аргументов.
func checkAll(cfg *config.Config, o checkOptions) []health.Row {
	projects, err := projectsOrDomains(cfg, nil)
	if err != nil {
		return nil
	}
	return checkRows(cfg, projects, o)
}

func checkData(cfg *config.Config, rows []health.Row, at time.Time) tui.BoardData {
	var d tui.BoardData
	d.Tab = "Проверка"
	if len(rows) == 0 {
		d.Status = "не проверялось"
		d.Empty = "Проверок ещё не было: git на машине, сайт, сертификат, почта и домен по сети - несколько секунд"
		return d
	}
	_, warn, bad := health.Counts(rows)
	d.Status = ui.Count(len(rows), "проект", "проекта", "проектов")
	if warn+bad > 0 {
		d.Status = fmt.Sprintf("%d требуют внимания", warn+bad)
	}
	d.Status += " \u00b7 проверено " + since(at)
	cols := usedCols(rows)
	nameW := nameWidth(rows) + 2
	head := ui.Pad("ПРОЕКТ", nameW)
	for _, col := range cols {
		head += ui.Pad(strings.ToUpper(col.name), col.width)
	}
	d.Columns = head
	d.Wide = nameW+len(cols)*12 > 56
	problems := warn + bad
	for _, r := range rows {
		row := tui.BoardRow{ID: r.Name, Group: "В ПОРЯДКЕ \u00b7 " + strconv.Itoa(len(rows)-problems)}
		if r.Level() >= level.Warn {
			row.Group = "ПРОБЛЕМЫ \u00b7 " + strconv.Itoa(problems)
		}
		row.Cells = ui.Fit(r.Name, nameW)
		for _, col := range cols {
			c := r.Get(col.name)
			cell := ""
			if c.Level != level.Skip && (c.Level != level.OK || r.Level() >= level.Warn || col.name != "git") {
				cell = level.Lamp(c.Level, ui.Clip(c.Text, col.width-3))
			}
			row.Cells += ui.Pad(cell, col.width)
		}
		card := &tui.Card{Title: r.Name}
		if p, err := cfg.Pick(r.Name); err == nil && p.URL != "" {
			card.Sub = strings.TrimPrefix(strings.TrimPrefix(p.URL, "https://"), "http://")
		}
		for _, c := range r.Checks {
			if c.Level < level.Warn {
				continue
			}
			text := c.Detail
			if text == "" {
				text = c.Text
			}
			card.Facts = append(card.Facts, tui.Fact{Label: level.Lamp(c.Level, c.Name), Value: text})
			if c.Fix != "" {
				card.Facts = append(card.Facts, tui.Fact{Label: "что делать", Value: c.Fix})
			}
		}
		if len(card.Facts) == 0 {
			card.Facts = []tui.Fact{{Value: ui.OK("\u25cf всё в порядке")}}
		}
		row.Card = card
		d.Rows = append(d.Rows, row)
	}
	d.Foot = machineNotes(cfg, machine.ForgottenAfter)
	return d
}

// since: давность проверки; старше суток - янтарная.
func since(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	d := time.Since(t)
	if d < time.Minute {
		return "только что"
	}
	if d > health.StaleAfter {
		return ui.Warn(ui.Short(d))
	}
	return ui.Short(d)
}
