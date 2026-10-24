package servers

import (
	"slices"
	"strings"

	"github.com/TimurTurdyev/tim/internal/app"
	"github.com/TimurTurdyev/tim/internal/health"
	"github.com/TimurTurdyev/tim/internal/level"
)

// Attention: беды серверов из кэша последнего состояния - "timcode: перезагрузка, порт".
func Attention(env *app.Env) []health.Alert {
	var out []health.Alert
	for _, t := range Targets(env, nil, false) {
		s, _, ok := saved(t.Alias)
		if !ok {
			continue
		}
		var names []string
		for _, c := range s.Findings {
			if c.Level >= level.Warn && !slices.Contains(names, c.Name) {
				names = append(names, c.Name)
			}
		}
		if len(names) > 0 {
			out = append(out, health.Alert{Level: s.level(), Text: t.Alias + ": " + strings.Join(names, ", ")})
		}
	}
	return out
}
