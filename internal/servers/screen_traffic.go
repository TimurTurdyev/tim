package servers

import (
	"strings"
	"time"

	"github.com/TimurTurdyev/tim/internal/app"
	"github.com/TimurTurdyev/tim/internal/level"
	"github.com/TimurTurdyev/tim/internal/tui"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// TrafficScreen: кто ходит на сайты сервера - находки, потом сайты; в панели сайта адреса, пути и ошибки.
func TrafficScreen(env *app.Env, target string, since time.Duration) tui.Screen {
	return tui.NewBoard(tui.BoardOpts{
		Crumbs:  []string{"Серверы", target, "трафик"},
		Loading: "читаю журналы запросов " + target + " за " + window(since) + ", только чтение",
		Command: func(*tui.BoardRow) string { return "tim server " + target + " --traffic" },
		Load:    func(bool) tui.BoardData { return trafficData(target, since) },
		Palette: func() (string, []tui.CommandEntry) { return recipePalette(env, target) },
	})
}

func trafficData(target string, since time.Duration) tui.BoardData {
	var d tui.BoardData
	r, err := collectTraffic(target, since)
	if err != nil {
		d.Status = "не достучался"
		d.Head = []string{level.Lamp(level.Bad, target+": "+sshError(err))}
		d.Empty = "отчёта нет"
		return d
	}
	d.Status = "за " + window(since)
	d.Head = []string{ui.Dim(trafficSources(r.Traffic))}
	d.Wide = true
	for _, c := range r.Findings {
		row := findingRow(c)
		row.Group = "НАХОДКИ"
		d.Rows = append(d.Rows, row)
	}
	var quiet []string
	for _, s := range r.Traffic.Sites {
		if s.Requests == 0 {
			quiet = append(quiet, s.Name)
			continue
		}
		d.Rows = append(d.Rows, tui.BoardRow{ID: "site " + s.File, Title: s.Name, Sub: s.File, Group: "САЙТЫ",
			Cells: siteLine(s, since), Pane: siteDetails(s, r.Owners)})
	}
	if len(quiet) > 0 {
		d.Foot = []string{ui.Dim("без запросов: " + strings.Join(quiet, ", "))}
	}
	d.Empty = "журналов запросов не видно"
	return d
}
