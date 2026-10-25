package sites

import (
	"fmt"
	"strings"

	"github.com/TimurTurdyev/tim/internal/config"
	"github.com/TimurTurdyev/tim/internal/health"
	"github.com/TimurTurdyev/tim/internal/level"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// Attention: беды сайтов из кэша проверок одной строкой на проверку - "почта: плохо у 2, неполно у 1".
// Сеть не трогается: главный открывается сразу.
func Attention(cfg *config.Config) []health.Alert {
	rows, _ := savedSites(cfg)
	var out []health.Alert
	for _, col := range siteCols {
		bad, warn := 0, 0
		badWord, warnWord := "", ""
		for _, r := range rows {
			c := r.Get(col.check)
			switch c.Level {
			case level.Bad:
				bad++
				badWord = c.Text
			case level.Warn:
				warn++
				warnWord = c.Text
			}
		}
		if bad+warn == 0 {
			continue
		}
		var parts []string
		if bad > 0 {
			parts = append(parts, fmt.Sprintf("%s у %d", badWord, bad))
		}
		if warn > 0 {
			parts = append(parts, fmt.Sprintf("%s у %d", warnWord, warn))
		}
		lvl := level.Warn
		if bad > 0 {
			lvl = level.Bad
		}
		out = append(out, health.Alert{Level: lvl, Text: col.check + ": " + strings.Join(parts, ", ")})
	}
	return out
}

// Troubled: сколько сайтов с бедой и сколько с предупреждением - для бейджа раздела.
func Troubled(cfg *config.Config) (bad, warn int) {
	rows, _ := savedSites(cfg)
	_, warn, bad = health.Counts(rows)
	return bad, warn
}

// Household: сайты из кэша проверок строками таблицы - для широкого главного экрана.
func Household(cfg *config.Config) []string {
	rows, at := savedSites(cfg)
	if len(rows) == 0 {
		return nil
	}
	out := []string{ui.Dim("ХОЗЯЙСТВО \u00b7 проверено " + since(at)),
		ui.Dim(ui.Pad("САЙТ", 18) + ui.Pad("ПРОЕКТ", 14) + ui.Pad("ОТВЕТ", 9) + ui.Pad("СЕРТ, ДН", 11) + ui.Pad("ПОЧТА", 12) + "ДОМЕН, ДН")}
	for _, r := range rows {
		p, err := cfg.Pick(r.Name)
		if err != nil {
			continue
		}
		line := ui.Fit(siteHost(p), 18) + ui.Fit(r.Name, 14)
		for i, col := range siteCols {
			c := r.Get(col.check)
			w := []int{9, 11, 12, 10}[i]
			cell := ""
			if c.Level != level.Skip {
				cell = level.Lamp(c.Level, strings.TrimSuffix(c.Text, " дн"))
			}
			line += ui.Pad(cell, w)
		}
		out = append(out, line)
	}
	return out
}
