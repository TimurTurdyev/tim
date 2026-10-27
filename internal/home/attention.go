package home

import (
	"strconv"
	"strings"
	"time"

	"github.com/TimurTurdyev/tim/internal/health"
	"github.com/TimurTurdyev/tim/internal/level"
	"github.com/TimurTurdyev/tim/internal/motd"
	"github.com/TimurTurdyev/tim/internal/servers"
	"github.com/TimurTurdyev/tim/internal/sites"
	"github.com/TimurTurdyev/tim/internal/tui"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// machine: беды, строки машины и бейджи разделов - то, что считается дольше остального.
func (h *Home) machine() tui.HomeMachine {
	rows, m := motd.Rows(h.env.Cfg, nil)
	out := tui.HomeMachine{Mood: m.Mood(time.Now()), Badges: map[string]string{}}
	counts := map[string][2]int{}
	add := func(lvl level.Level, text, dest, key string, open func() tui.Action) {
		title := ui.Warn("\u25b2 " + text)
		c := counts[key]
		if lvl == level.Bad {
			title = ui.Bad("\u2715 " + text)
			c[0]++
		} else {
			c[1]++
		}
		counts[key] = c
		out.Attention = append(out.Attention, tui.Hit{Kind: tui.KindAttention, Title: title, Hint: dest, Open: open})
	}
	// Порядок: память и диск машины, сайты, обход и страж, серверы.
	sitesOpen := func() tui.Action { return tui.Action{Push: sites.SitesScreen(h.env)} }
	sitesDone := false
	for _, w := range m.Warnings() {
		if (w.Kind == health.Bypass || w.Kind == health.Guard) && !sitesDone {
			for _, a := range sites.Attention(h.env.Cfg) {
				add(a.Level, a.Text, "Сайты", "w", sitesOpen)
			}
			sitesDone = true
		}
		dest, key, open := h.trouble(w, m.MemHolder)
		add(level.Warn, w.Text, dest, key, open)
	}
	if !sitesDone {
		for _, a := range sites.Attention(h.env.Cfg) {
			add(a.Level, a.Text, "Сайты", "w", sitesOpen)
		}
	}
	for _, a := range sites.DomainAttention(h.env.Cfg) {
		domain, _, _ := strings.Cut(a.Text, ":")
		add(a.Level, a.Text, "Сайты", "w", func() tui.Action { return tui.Action{Push: sites.DomainScreen(h.env, domain)} })
	}
	for _, a := range servers.Attention(h.env) {
		target, _, _ := strings.Cut(a.Text, ":")
		add(a.Level, a.Text, "Серверы", "s", func() tui.Action {
			return tui.Action{Push: servers.Screen(h.env, target)}
		})
	}
	out.Household = append(sites.Household(h.env.Cfg), servers.Household(h.env)...)
	// У Сайтов бейдж - число сайтов с бедой, а не строк ВНИМАНИЯ.
	if bad, warn := sites.Troubled(h.env.Cfg); bad+warn > 0 {
		counts["w"] = [2]int{bad, warn}
	}
	for key, c := range counts {
		out.Badges[key] = badge(c[0], c[1])
	}
	for _, r := range rows {
		if r.Label == "" {
			continue
		}
		dest, open := h.lineDest(r.Dest)
		out.Machine = append(out.Machine, tui.Hit{Kind: tui.KindMachine, Title: r.Label, Note: r.Value, Hint: dest, Open: open})
	}
	return out
}

// badge: число бед раздела лампами, сначала ошибки, потом предупреждения.
func badge(bad, warn int) string {
	var parts []string
	if bad > 0 {
		parts = append(parts, ui.Bad("\u2715 "+strconv.Itoa(bad)))
	}
	if warn > 0 {
		parts = append(parts, ui.Warn("\u25b2 "+strconv.Itoa(warn)))
	}
	return strings.Join(parts, " ")
}
