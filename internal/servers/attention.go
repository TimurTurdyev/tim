package servers

import (
	"slices"
	"strings"

	"github.com/TimurTurdyev/tim/internal/app"
	"github.com/TimurTurdyev/tim/internal/health"
	"github.com/TimurTurdyev/tim/internal/level"
	"github.com/TimurTurdyev/tim/internal/ui"
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
		// Новое с прошлого раза - первым: перемена важнее вечной находки.
		names = append(pendingWords(t.Alias), names...)
		lvl := s.level()
		if lvl < level.Warn && len(names) > 0 {
			lvl = level.Warn
		}
		if len(names) > 0 {
			out = append(out, health.Alert{Level: lvl, Text: t.Alias + ": " + strings.Join(names, ", ")})
		}
	}
	return out
}

// Household: серверы из кэша одной строкой каждый - для широкого главного экрана.
func Household(env *app.Env) []string {
	var out []string
	var unchecked []string
	for _, t := range Targets(env, nil, false) {
		s, _, ok := saved(t.Alias)
		if !ok {
			unchecked = append(unchecked, t.Alias)
			continue
		}
		var finds []string
		for _, c := range s.Findings {
			if c.Level >= level.Warn {
				finds = append(finds, c.Name)
			}
		}
		line := ui.Fit(t.Alias, 18) + ui.Dim(s.Report.OS+" \u00b7 "+ui.Short(s.Report.Uptime))
		if len(finds) > 0 {
			line += ui.Dim(" \u00b7 ") + level.Lamp(s.level(), strings.Join(finds, ", "))
		}
		out = append(out, line)
	}
	if len(out) == 0 && len(unchecked) == 0 {
		return nil
	}
	head := []string{ui.Dim(ui.Pad("СЕРВЕР", 18) + "ОС \u00b7 РАБОТАЕТ \u00b7 НАХОДКИ")}
	if len(unchecked) > 0 {
		out = append(out, ui.Dim(strings.Join(unchecked, ", ")+" - не проверялись"))
	}
	return append(head, out...)
}
