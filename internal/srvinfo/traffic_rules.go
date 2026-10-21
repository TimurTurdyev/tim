package srvinfo

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/TimurTurdyev/tim/internal/health"
	"github.com/TimurTurdyev/tim/internal/level"
)

// Пороги трафика: на десятке запросов проценты - шум, доли считаются от minHits.
const (
	minHits    = 20
	slowP95    = 2 * time.Second
	hogShare   = 30 // один адрес даёт столько процентов запросов
	errorShare = 1  // процент 5xx, с которого это находка; в десять раз больше - беда
)

// EvaluateTraffic: что в трафике не так - ошибки сервера, один адрес вместо посетителей, медленные
// ответы, запросы в пустоту. Тихий сайт без запросов - к сведению: журнал может писаться не сюда.
func EvaluateTraffic(t Traffic) []health.Check {
	var f findings
	for _, s := range t.Sites {
		f.site(s)
	}
	slices.SortStableFunc(f, func(a, b health.Check) int { return cmp.Compare(b.Level, a.Level) })
	return f
}

func (f *findings) site(s SiteTraffic) {
	name := s.Name
	if s.Requests == 0 {
		f.add("тишина", level.Info, name, name+": за окно ни одного запроса", "")
		return
	}
	if s.Requests < minHits {
		return
	}
	errLog := strings.Replace(s.File, "access.log", "error.log", 1)
	if share := Share(s.Classes[5], s.Requests); share >= errorShare {
		l := level.Warn
		if share >= errorShare*10 {
			l = level.Bad
		}
		f.add("5xx", l, fmt.Sprintf("%s %d%%", name, share),
			fmt.Sprintf("%s: %d ответов 5xx из %d, чаще всего %s", name, s.Classes[5], s.Requests, countText(s.Errors)),
			"причина в журнале ошибок: tail -n 100 "+errLog)
	}
	if len(s.ByIP) > 0 && Share(s.ByIP[0].N, s.Requests) >= hogShare && s.Requests >= 100 {
		ip := s.ByIP[0]
		f.add("один адрес", level.Warn, fmt.Sprintf("%s %d%%", name, Share(ip.N, s.Requests)),
			fmt.Sprintf("%s: %s дал %d из %d запросов", name, ip.Key, ip.N, s.Requests),
			"если это не свой мониторинг: limit_req в nginx или ufw deny from "+ip.Key)
	}
	if s.Timed && s.P95 >= slowP95 {
		fix := "tim http <адрес страницы> покажет фазы; кэш страниц, opcache, индексы в базе"
		if strings.Contains(name, ".") {
			fix = "tim http https://" + name + "/<путь> покажет фазы; кэш страниц, opcache, индексы в базе"
		}
		f.add("медленно", level.Warn, fmt.Sprintf("%s p95 %.1f с", name, s.P95.Seconds()),
			fmt.Sprintf("%s: половина ответов дольше %.2f с, каждый двадцатый дольше %.1f с; дольше всех %s", name, s.P50.Seconds(), s.P95.Seconds(), slowText(s.Slow)), fix)
	}
	if share := Share(s.Classes[4], s.Requests); share >= 20 && s.Requests >= 50 {
		f.add("4xx", level.Info, fmt.Sprintf("%s %d%%", name, share),
			fmt.Sprintf("%s: %d%% запросов в пустоту или без прав, чаще всего %s", name, share, countText(s.Missing)),
			"битые ссылки или сканеры: tim audit "+name+" проверит ссылки с главной")
	}
	if share := Share(s.Bots, s.Requests); share >= 50 && s.Requests >= 50 {
		f.add("боты", level.Info, fmt.Sprintf("%s %d%%", name, share), fmt.Sprintf("%s: %d%% запросов - боты и скрипты, а не люди", name, share), "")
	}
}

// countText: первые пути со счётом - "/api/pay x30, /x x2".
func countText(list []Count) string {
	if len(list) == 0 {
		return "-"
	}
	var parts []string
	for _, c := range list[:min(3, len(list))] {
		parts = append(parts, fmt.Sprintf("%s x%d", c.Key, c.N))
	}
	return strings.Join(parts, ", ")
}

// slowText: пути, которые отвечают дольше секунды, - быстрые рядом с ними только путают.
func slowText(list []Timing) string {
	var parts []string
	for _, t := range list {
		if t.Avg >= time.Second && len(parts) < 3 {
			parts = append(parts, fmt.Sprintf("%s %.1f с", t.Path, t.Avg.Seconds()))
		}
	}
	if len(parts) == 0 {
		return "-"
	}
	return strings.Join(parts, ", ")
}
