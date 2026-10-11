package sites

import (
	"fmt"
	"strings"
	"sync"

	"github.com/TimurTurdyev/tim/internal/app"
	"github.com/TimurTurdyev/tim/internal/config"
	"github.com/TimurTurdyev/tim/internal/health"
	"github.com/TimurTurdyev/tim/internal/level"
	"github.com/TimurTurdyev/tim/internal/netinfo"
	"github.com/TimurTurdyev/tim/internal/tui"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// SitesScreen: сайты проектов - ответ, сертификат, почта, домен; enter - экран домена,
// d - любой другой домен.
func SitesScreen(env *app.Env) tui.Screen {
	var (
		mu   sync.Mutex
		rows []health.Row
	)
	return tui.NewBoard(tui.BoardOpts{
		Crumbs:  []string{"Сайты"},
		Loading: "проверяю сайты проектов: ответ, сертификат, почта, домен - несколько секунд",
		Command: func(row *tui.BoardRow) string {
			if row != nil {
				return "tim domain " + row.ID
			}
			return "tim domain <домен>"
		},
		Load: func(fresh bool) tui.BoardData {
			mu.Lock()
			defer mu.Unlock()
			if rows == nil || fresh {
				rows = siteRows(env.Cfg)
			}
			return sitesData(env.Cfg, rows)
		},
		Actions: []tui.BoardAction{
			{Key: "enter", Label: "домен", Do: func(row *tui.BoardRow) tui.Action {
				d, err := DomainOf(env, row.ID)
				if err != nil {
					return tui.Action{Command: "domain " + row.ID}
				}
				return tui.Action{Push: DomainScreen(env, d)}
			}},
			{Key: "d", Label: "любой домен", Free: true, Do: func(*tui.BoardRow) tui.Action {
				return tui.Action{Command: "domain "}
			}},
		},
	})
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

func sitesData(cfg *config.Config, rows []health.Row) tui.BoardData {
	var d tui.BoardData
	_, warn, bad := health.Counts(rows)
	d.Status = ui.Count(len(rows), "сайт", "сайта", "сайтов")
	if warn+bad > 0 {
		d.Status += fmt.Sprintf(" · %d требуют внимания", warn+bad)
	}
	hosts := make([]string, len(rows))
	hostW := len("САЙТ")
	for i, r := range rows {
		if p, err := cfg.Pick(r.Name); err == nil {
			hosts[i] = siteHost(p)
		}
		hostW = min(max(hostW, ui.Width(hosts[i])), 32)
	}
	nameW := nameWidth(rows)
	cols := usedCols(rows)
	head := ui.Pad("САЙТ", hostW+2) + ui.Pad("ПРОЕКТ", nameW+2)
	for _, col := range cols {
		head += ui.Pad(strings.ToUpper(col.name), col.width)
	}
	if len(rows) > 0 {
		d.Columns = ui.Dim(head)
	}
	for i, r := range rows {
		row := tui.BoardRow{ID: hosts[i], Title: hosts[i], Sub: r.Name}
		row.Cells = ui.Fit(hosts[i], hostW) + "  " + ui.Dim(ui.Fit(r.Name, nameW)) + "  "
		for _, col := range cols {
			c := r.Get(col.name)
			row.Cells += ui.Pad(level.Lamp(c.Level, ui.Clip(c.Text, col.width-3)), col.width)
		}
		row.Pane = append(problemLines(r), ui.Dim("→ enter - домен целиком: имена, адреса, почта"))
		d.Rows = append(d.Rows, row)
	}
	d.Empty = "у проектов нет адреса сайта: tim sync возьмёт его из .env, tim sync --remote - из nginx на серверах. d - проверить любой домен"
	return d
}

// problemLines: всё, что не в порядке, с подробностью, если она есть. Карточка не спорит со строкой:
// предупреждение без подробностей тоже попадает сюда.
func problemLines(r health.Row) []string {
	var out []string
	for _, c := range r.Checks {
		if c.Level < level.Warn {
			continue
		}
		text := c.Detail
		if text == "" {
			text = c.Text
		}
		out = append(out, level.Lamp(c.Level, c.Name+": "+text))
	}
	if len(out) == 0 {
		return []string{ui.OK("● всё в порядке")}
	}
	return out
}

// nameWidth: колонка имён по самому длинному, но не шире 28.
func nameWidth(rows []health.Row) int {
	w := len("ПРОЕКТ")
	for _, r := range rows {
		w = max(w, ui.Width(r.Name))
	}
	return min(w, 28)
}
