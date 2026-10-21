package audit

import (
	"strings"

	"github.com/TimurTurdyev/tim/internal/level"
)

// ParseRobots: закрыт ли сайт для всех роботов и какие sitemap объявлены.
func ParseRobots(body string) (closed bool, sitemaps []string) {
	forAll := false
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if i := strings.Index(line, "#"); i >= 0 {
			line = strings.TrimSpace(line[:i])
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key, value = strings.ToLower(strings.TrimSpace(key)), strings.TrimSpace(value)
		switch key {
		case "user-agent":
			forAll = value == "*"
		case "disallow":
			if forAll && value == "/" {
				closed = true
			}
		case "sitemap":
			sitemaps = append(sitemaps, value)
		}
	}
	return closed, sitemaps
}

// indexing: robots.txt, sitemap и мягкая 404 - классика после запуска: сайт выложили, а от поисковиков
// не открыли.
func indexing(s *site) []Finding {
	var out []Finding
	add := func(l level.Level, text, why, fix string) {
		out = append(out, Finding{Area: "индексация", Level: l, Text: text, Why: why, Fix: fix})
	}
	robots := s.get(s.base + "/robots.txt")
	var sitemaps []string
	switch {
	case robots.status != 200 || strings.Contains(strings.ToLower(robots.header.Get("Content-Type")), "text/html"):
		add(level.Info, "нет robots.txt", "поисковики обходят всё подряд, включая служебные страницы", "robots.txt со строкой Sitemap: и запретами на служебные пути")
	default:
		var closed bool
		closed, sitemaps = ParseRobots(robots.body)
		if closed {
			add(level.Bad, "сайт закрыт от индексации", "robots.txt запрещает всё всем роботам: в поиске сайта нет", "убрать Disallow: / для User-agent: * - обычно это забытая настройка с тестового стенда")
		}
	}
	if len(sitemaps) == 0 {
		if sm := s.get(s.base + "/sitemap.xml"); sm.status == 200 && (strings.Contains(sm.body, "<urlset") || strings.Contains(sm.body, "<sitemapindex")) {
			sitemaps = append(sitemaps, s.base+"/sitemap.xml")
		}
	}
	if len(sitemaps) == 0 {
		add(level.Info, "нет sitemap", "новые страницы попадут в поиск позже", "sitemap.xml и строка Sitemap: в robots.txt")
	}
	return out
}

// canonicalHost: www и без www должны вести на один адрес, иначе поисковик видит два сайта с одним
// содержимым.
func canonicalHost(s *site) []Finding {
	other := "https://www." + s.host
	if strings.HasPrefix(s.host, "www.") {
		other = "https://" + strings.TrimPrefix(s.host, "www.")
	}
	if net := strings.Count(s.host, "."); net == 0 || strings.Contains(s.host, ":") || isIP(s.host) {
		return nil
	}
	a := s.get(other+"/", true)
	if a.err != nil || a.status != 200 {
		return nil
	}
	if hostOf(a.url) == s.host {
		return nil
	}
	return []Finding{{Area: "индексация", Level: level.Warn, Text: "www и без www - разные адреса",
		Why: "поисковик делит вес между " + s.host + " и " + hostOf(a.url) + ", ссылки ведут на разные версии",
		Fix: "301 с одного варианта на другой в nginx и canonical на страницах"}}
}

func isIP(host string) bool {
	for _, c := range host {
		if c != '.' && (c < '0' || c > '9') {
			return false
		}
	}
	return true
}
