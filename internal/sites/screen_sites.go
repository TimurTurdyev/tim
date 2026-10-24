package sites

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/TimurTurdyev/tim/internal/app"
	"github.com/TimurTurdyev/tim/internal/config"
	"github.com/TimurTurdyev/tim/internal/health"
	"github.com/TimurTurdyev/tim/internal/level"
	"github.com/TimurTurdyev/tim/internal/netinfo"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/tui"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// siteCols: колонки Сайтов - проверка и её заголовок.
var siteCols = []struct{ check, head string }{
	{"сайт", "ОТВЕТ"}, {"сертификат", "СЕРТ, ДН"}, {"почта", "ПОЧТА"}, {"домен", "ДОМЕН, ДН"},
}

// SitesScreen: сайты проектов - ответ, сертификат, почта, домен. Открывается из кэша проверок,
// r проверяет заново; домен, которого нет в списке, проверяется по enter из поиска.
func SitesScreen(env *app.Env) tui.Screen {
	var (
		mu   sync.Mutex
		rows []health.Row
		at   time.Time
	)
	return tui.NewBoard(tui.BoardOpts{
		Crumbs:  []string{"Сайты"},
		Loading: "проверяю сайты проектов: ответ, сертификат, почта, домен - несколько секунд",
		Search:  true,
		Order:   "проблемные сверху",
		Command: func(row *tui.BoardRow) string {
			if row != nil {
				return "tim domain " + row.ID
			}
			return "tim check --net"
		},
		Load: func(fresh bool) tui.BoardData {
			mu.Lock()
			defer mu.Unlock()
			if rows == nil && !fresh {
				rows, at = savedSites(env.Cfg)
			}
			if fresh || rows == nil {
				rows, at = siteRows(env.Cfg), time.Now()
			}
			return sitesData(env.Cfg, rows, at)
		},
		Miss: func(q string) (string, tui.Action) {
			return "проверит " + q + " как домен", tui.Action{Push: DomainScreen(env, q)}
		},
		Actions: []tui.BoardAction{
			{Key: "enter", Label: "домен", Do: func(row *tui.BoardRow) tui.Action {
				d, err := DomainOf(env, row.Find)
				if err != nil {
					return tui.Action{Command: "domain " + row.ID}
				}
				return tui.Action{Push: DomainScreen(env, d)}
			}},
			{Key: "w", Label: "браузер", Doing: "открываю сайт", Run: func(row *tui.BoardRow) (string, error) {
				return row.ID + ": открыт в браузере", runner.OpenBrowser("https://" + row.ID)
			}},
			{Key: "c", Label: "проверить", Doing: "проверяю сайт", Run: func(row *tui.BoardRow) (string, error) {
				p, err := env.Cfg.Pick(row.Find)
				if err != nil {
					return "", err
				}
				r := checkRows(env.Cfg, []*config.Project{p}, checkOptions{net: true})
				mu.Lock()
				for i := range rows {
					if len(r) > 0 && rows[i].Name == r[0].Name {
						rows[i] = r[0]
					}
				}
				mu.Unlock()
				if len(r) == 0 {
					return row.ID + ": проверять нечего", nil
				}
				return row.ID + ": " + verdict(r[0]), nil
			}},
			{Key: "s", Label: "ssh", Note: "закроет tim", Do: func(row *tui.BoardRow) tui.Action {
				return tui.Action{Exit: func() { env.After([]string{"ssh", row.Find}) }}
			}},
		},
	})
}

// savedSites: сайты проектов из кэша проверок.
func savedSites(cfg *config.Config) ([]health.Row, time.Time) {
	rows, at := savedRows(cfg)
	kept := rows[:0]
	for _, r := range rows {
		if p, ok := cfg.Project(r.Name); ok && siteHost(p) != "" && r.Get("сайт").Level != level.Skip {
			kept = append(kept, r)
		}
	}
	if len(kept) == 0 {
		return nil, at
	}
	return kept, at
}

// siteRows: проверки сайтов проектов без git; проекты без адреса и локальные пропускаются.
func siteRows(cfg *config.Config) []health.Row {
	var withSite []*config.Project
	for i := range cfg.Projects {
		if siteHost(&cfg.Projects[i]) != "" {
			withSite = append(withSite, &cfg.Projects[i])
		}
	}
	return checkRows(cfg, withSite, checkOptions{net: true})
}

func siteHost(p *config.Project) string {
	h, _ := netinfo.HostPort(p.URL, "")
	return h
}

func sitesData(cfg *config.Config, rows []health.Row, at time.Time) tui.BoardData {
	var d tui.BoardData
	_, warn, bad := health.Counts(rows)
	d.Status = ui.Count(len(rows), "сайт", "сайта", "сайтов")
	if warn+bad > 0 {
		d.Status += fmt.Sprintf(" \u00b7 %d с бедой", warn+bad)
	}
	d.Status += " \u00b7 проверено " + since(at)
	hosts := make([]string, len(rows))
	hostW, nameW := len("САЙТ"), len("ПРОЕКТ")
	for i, r := range rows {
		if p, err := cfg.Pick(r.Name); err == nil {
			hosts[i] = siteHost(p)
		}
		hostW = max(hostW, ui.Width(hosts[i]))
		nameW = max(nameW, ui.Width(r.Name))
	}
	head := ui.Pad("САЙТ", hostW+3) + ui.Pad("ПРОЕКТ", nameW+3)
	for _, col := range siteCols {
		head += ui.Pad(col.head, 12)
	}
	d.Columns = head
	d.Wide = true
	for i, r := range rows {
		row := tui.BoardRow{ID: hosts[i], Find: r.Name}
		row.Cells = ui.Fit(hosts[i], hostW+3) + ui.Dim(ui.Fit(r.Name, nameW+3))
		for _, col := range siteCols {
			c := r.Get(col.check)
			cell := ""
			if c.Level != level.Skip {
				// Единица в шапке колонки: "СЕРТ, ДН" - в ячейке только число.
				cell = level.Lamp(c.Level, ui.Clip(strings.TrimSuffix(c.Text, " дн"), 9))
			}
			row.Cells += ui.Pad(cell, 12)
		}
		row.Card = siteCard(cfg, hosts[i], r)
		d.Rows = append(d.Rows, row)
	}
	d.Empty = "У проектов нет адреса сайта: tim sync возьмёт его из .env, tim sync --remote - из nginx на серверах"
	return d
}

// siteCard: что не так с сайтом, что делать и где он лежит.
func siteCard(cfg *config.Config, host string, r health.Row) *tui.Card {
	c := &tui.Card{Title: host, Sub: r.Name, Enter: "домен целиком: имена, адреса, сертификат, почта"}
	p, err := cfg.Pick(r.Name)
	if err == nil {
		c.Path = p.URL
	}
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
	if err == nil && p.SSH != "" {
		c.Facts = append(c.Facts, tui.Fact{Label: "сервер", Value: p.SSH})
		if p.Remote != "" {
			c.Facts = append(c.Facts, tui.Fact{Value: ui.Dim(p.Remote)})
		}
	}
	if len(c.Facts) == 0 {
		c.About = []string{ui.OK("\u25cf всё в порядке")}
	}
	return c
}

// nameWidth: колонка имён по самому длинному, но не шире 28.
func nameWidth(rows []health.Row) int {
	w := len("ПРОЕКТ")
	for _, r := range rows {
		w = max(w, ui.Width(r.Name))
	}
	return min(w, 28)
}
