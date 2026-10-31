package sites

import (
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/TimurTurdyev/tim/internal/app"
	"github.com/TimurTurdyev/tim/internal/config"
	"github.com/TimurTurdyev/tim/internal/health"
	"github.com/TimurTurdyev/tim/internal/level"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/tui"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// siteCols: колонки Сайтов - проверка и её заголовок.
var siteCols = []struct{ check, head string }{
	{"сайт", "ОТВЕТ"}, {"сертификат", "СЕРТ, ДН"}, {"почта", "ПОЧТА"}, {"домен", "ДОМЕН, ДН"},
}

// SitesScreen: сайты из списка sites - ответ, сертификат, почта, домен. Открывается из кэша проверок,
// r проверяет заново; домен, которого нет в списке, проверяется по enter из поиска.
func SitesScreen(env *app.Env) tui.Screen {
	var (
		mu   sync.Mutex
		rows []health.Row
		at   time.Time
	)
	return tui.NewBoard(tui.BoardOpts{
		Crumbs:  []string{"Сайты"},
		Loading: "проверяю сайты: ответ, сертификат, почта, домен - несколько секунд",
		Search:  true,
		Order:   "проблемные сверху",
		Command: func(row *tui.BoardRow) string {
			if row != nil {
				return "tim domain " + row.ID
			}
			return "tim check"
		},
		Load: func(fresh bool) tui.BoardData {
			mu.Lock()
			defer mu.Unlock()
			if rows == nil && !fresh {
				rows, at = savedSites(env.Cfg)
			}
			if fresh || rows == nil {
				rows, at = checkRows(siteHosts(env.Cfg, nil), nil), time.Now()
			}
			return sitesData(rows, at)
		},
		Miss: func(q string) (string, tui.Action) {
			return "проверит " + q + " как домен", tui.Action{Push: DomainScreen(env, q)}
		},
		Actions: []tui.BoardAction{
			{Key: "enter", Label: "домен", Do: func(row *tui.BoardRow) tui.Action { return tui.Action{Push: DomainScreen(env, row.ID)} }},
			{Key: "w", Label: "браузер", Doing: "открываю сайт", Run: func(row *tui.BoardRow) (string, error) {
				return row.ID + ": открыт в браузере", runner.OpenBrowser("https://" + row.ID)
			}},
			{Key: "c", Label: "проверить", Doing: "проверяю сайт", Run: func(row *tui.BoardRow) (string, error) {
				var shared DomainChecks
				r := SiteRow(row.ID, &shared)
				mu.Lock()
				for i := range rows {
					if rows[i].Name == r.Name {
						rows[i] = r
					}
				}
				mu.Unlock()
				return row.ID + ": " + verdict(r), nil
			}},
		},
	})
}

// savedSites: сайты из кэша проверок - только те, что сейчас в списке.
func savedSites(cfg *config.Config) ([]health.Row, time.Time) {
	hosts := siteHosts(cfg, nil)
	var rows []health.Row
	var oldest time.Time
	for _, s := range health.SavedRows() {
		if !slices.Contains(hosts, s.Name) || s.Get("сайт").Level == level.Skip {
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

// verdict: исход проверки одного сайта для строки состояния.
func verdict(r health.Row) string {
	switch r.Level() {
	case level.Bad, level.Warn:
		return ui.Count(len(r.Problems()), "проблема", "проблемы", "проблем")
	}
	return "в порядке"
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

func sitesData(rows []health.Row, at time.Time) tui.BoardData {
	var d tui.BoardData
	_, warn, bad := health.Counts(rows)
	d.Status = ui.Count(len(rows), "сайт", "сайта", "сайтов")
	if warn+bad > 0 {
		d.Status += fmt.Sprintf(" · %d с бедой", warn+bad)
	}
	d.Status += " · проверено " + since(at)
	hostW := len("САЙТ")
	for _, r := range rows {
		hostW = max(hostW, ui.Width(r.Name))
	}
	head := ui.Pad("САЙТ", hostW+3)
	for _, col := range siteCols {
		head += ui.Pad(col.head, 12)
	}
	d.Columns = head
	d.Wide = true
	for _, r := range rows {
		row := tui.BoardRow{ID: r.Name}
		row.Cells = ui.Fit(r.Name, hostW+3)
		for _, col := range siteCols {
			c := r.Get(col.check)
			cell := ""
			if c.Level != level.Skip {
				// Единица в шапке колонки: "СЕРТ, ДН" - в ячейке только число.
				cell = level.Lamp(c.Level, ui.Clip(strings.TrimSuffix(c.Text, " дн"), 9))
			}
			row.Cells += ui.Pad(cell, 12)
		}
		row.Card = siteCard(r)
		d.Rows = append(d.Rows, row)
	}
	d.Empty = "Сайтов нет. Добавь их в конфиг: sites = [\"example.com\"] - или проверь любой: набери домен в / и enter"
	return d
}

// siteCard: что не так с сайтом и что делать.
func siteCard(r health.Row) *tui.Card {
	c := &tui.Card{Title: r.Name, Path: "https://" + r.Name, Enter: "домен целиком: имена, адреса, записи, сертификат, почта"}
	for _, ch := range r.Checks {
		if ch.Level < level.Warn {
			continue
		}
		// Подпись уже называет проверку: "почта: почта: нет DMARC" - лишнее.
		text := strings.TrimPrefix(ch.Detail, ch.Name+": ")
		if text == "" {
			text = ch.Text
		}
		c.Facts = append(c.Facts, tui.Fact{Label: level.Lamp(ch.Level, ch.Name), Value: text})
		if ch.Fix != "" {
			c.Facts = append(c.Facts, tui.Fact{Label: "что делать", Value: ch.Fix})
		}
	}
	if len(c.Facts) == 0 {
		c.About = []string{ui.OK("● всё в порядке")}
	}
	return c
}
