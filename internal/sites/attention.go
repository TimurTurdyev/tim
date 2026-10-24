package sites

import (
	"fmt"
	"strings"

	"github.com/TimurTurdyev/tim/internal/config"
	"github.com/TimurTurdyev/tim/internal/health"
	"github.com/TimurTurdyev/tim/internal/level"
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
