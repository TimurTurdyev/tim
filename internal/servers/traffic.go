package servers

import (
	"fmt"
	"strings"
	"time"

	"github.com/TimurTurdyev/tim/internal/asn"
	"github.com/TimurTurdyev/tim/internal/health"
	"github.com/TimurTurdyev/tim/internal/srvinfo"
	"github.com/TimurTurdyev/tim/internal/stats"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// sparkWidth: ширина графика запросов по минутам.
const sparkWidth = 24

// trafficReport: трафик, находки и владельцы самых частых адресов.
type trafficReport struct {
	Traffic  srvinfo.Traffic       `json:"traffic"`
	Findings []health.Check        `json:"findings"`
	Owners   map[string]asn.ASInfo `json:"owners,omitempty"`
}

// collectTraffic: журналы по ssh, потом владельцы сетей самых частых адресов - сразу видно, бот это
// или свой мониторинг.
func collectTraffic(target string, since time.Duration) (*trafficReport, error) {
	t, err := srvinfo.CollectTraffic(target, since)
	if err != nil {
		return nil, err
	}
	r := &trafficReport{Traffic: t, Findings: srvinfo.EvaluateTraffic(t)}
	var ips []string
	for _, s := range t.Sites {
		for _, c := range s.ByIP[:min(3, len(s.ByIP))] {
			ips = append(ips, c.Key)
		}
	}
	r.Owners = asn.Cymru(ips)
	return r, nil
}

// printTraffic: находки, потом каждый сайт - строка со сводкой и подробности.
func printTraffic(target string, since time.Duration, asJSON bool) error {
	r, err := collectTraffic(target, since)
	if err != nil {
		return ui.Errorf("%s: %v; маршрут и вход проверит tim ssh %s", target, err, target)
	}
	if asJSON {
		return ui.JSON(r)
	}
	ui.Println(ui.Label(target) + "  " + ui.Dim("за "+window(since)+": "+trafficSources(r.Traffic)))
	printFindings(r.Findings)
	for _, s := range r.Traffic.Sites {
		ui.Println("")
		ui.Println(siteLine(s))
		for _, l := range siteDetails(s, r.Owners) {
			ui.Println("  " + l)
		}
	}
	return nil
}

// trafficSources: сколько журналов и запросов; без журналов - где их обычно искать.
func trafficSources(t srvinfo.Traffic) string {
	if len(t.Sites) == 0 {
		return "журналов запросов не видно: /var/log/nginx читает группа adm, панели пишут в папку пользователя"
	}
	total := 0
	for _, s := range t.Sites {
		total += s.Requests
	}
	return fmt.Sprintf("%d %s, %d %s", len(t.Sites), ui.Plural(len(t.Sites), "журнал", "журнала", "журналов"),
		total, ui.Plural(total, "запрос", "запроса", "запросов"))
}

// siteLine: сайт, сколько запросов и как они легли по минутам.
func siteLine(s srvinfo.SiteTraffic) string {
	line := ui.Label(ui.Pad(s.Name, 22))
	if s.Requests == 0 {
		return line + ui.Dim("запросов нет")
	}
	vals := make([]float64, len(s.Minutes))
	for i, n := range s.Minutes {
		vals[i] = float64(n)
	}
	line += fmt.Sprintf("%d %s, %s в мин  ", s.Requests, ui.Plural(s.Requests, "запрос", "запроса", "запросов"), s.PerMinute()) + stats.Bars(vals, sparkWidth)
	if s.Partial {
		line += ui.Dim("  хвост журнала с " + s.From.Local().Format("15:04"))
	}
	return line
}

// siteDetails: ответы по классам, адреса с владельцами, пути, время ответа, ошибки и 404.
func siteDetails(s srvinfo.SiteTraffic, owners map[string]asn.ASInfo) []string {
	if s.Requests == 0 {
		return nil
	}
	row := func(label, text string) string { return ui.Pad(label, 9) + text }
	var out []string
	var classes []string
	for i := 2; i <= 5; i++ {
		if s.Classes[i] == 0 {
			continue
		}
		part := fmt.Sprintf("%dxx %d%%", i, srvinfo.Share(s.Classes[i], s.Requests))
		if i == 5 {
			part = ui.Bad(part)
		}
		classes = append(classes, part)
	}
	out = append(out, row("ответы", strings.Join(classes, "  ")+ui.Dim(fmt.Sprintf("  %d %s, боты %d%%, %s", s.IPs,
		ui.Plural(s.IPs, "адрес", "адреса", "адресов"), srvinfo.Share(s.Bots, s.Requests), ui.Bytes(s.Bytes)))))
	// Адреса с одним-двумя запросами - не "кто ходит", а шум; первый остаётся всегда.
	var ips []string
	for i, c := range s.ByIP[:min(3, len(s.ByIP))] {
		if i > 0 && srvinfo.Share(c.N, s.Requests) < 1 {
			break
		}
		part := fmt.Sprintf("%s %d%%", c.Key, srvinfo.Share(c.N, s.Requests))
		if o, ok := owners[c.Key]; ok && o.Name != "" {
			part += ui.Dim(" " + ui.Clip(o.Name, 20))
		}
		ips = append(ips, part)
	}
	out = append(out, row("адреса", strings.Join(ips, "  ")))
	var paths []string
	for _, c := range s.ByPath[:min(4, len(s.ByPath))] {
		paths = append(paths, fmt.Sprintf("%s %d%%", ui.Clip(c.Key, 28), srvinfo.Share(c.N, s.Requests)))
	}
	out = append(out, row("пути", strings.Join(paths, "  ")))
	if s.Timed {
		var slow []string
		for _, t := range s.Slow[:min(3, len(s.Slow))] {
			slow = append(slow, fmt.Sprintf("%s %.1f с x%d", ui.Clip(t.Path, 28), t.Avg.Seconds(), t.N))
		}
		out = append(out, row("ответ", fmt.Sprintf("p50 %.2f с, p95 %.2f с", s.P50.Seconds(), s.P95.Seconds())+ui.Dim("  дольше всех "+strings.Join(slow, ", "))))
	}
	if len(s.Errors) > 0 {
		out = append(out, row("5xx", counts(s.Errors)))
	}
	if len(s.Missing) > 0 {
		out = append(out, row("404", counts(s.Missing)))
	}
	return out
}

func counts(list []srvinfo.Count) string {
	var parts []string
	for _, c := range list[:min(4, len(list))] {
		parts = append(parts, fmt.Sprintf("%s x%d", ui.Clip(c.Key, 28), c.N))
	}
	return strings.Join(parts, "  ")
}
