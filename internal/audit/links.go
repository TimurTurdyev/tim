package audit

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/TimurTurdyev/tim/internal/level"
	"github.com/TimurTurdyev/tim/internal/par"
)

// Битые ссылки: внутренние ссылки главной, страниц на шаг глубже и из sitemap. Только свой сайт и не
// больше linkLimit адресов: аудит не должен превращаться в обход всего сайта.
const (
	linkLimit  = 150
	deepPages  = 10
	sitemapMax = 50
)

var locRe = regexp.MustCompile(`<loc>\s*([^<\s]+)\s*</loc>`)

// ParseSitemap: адреса страниц из sitemap.xml.
func ParseSitemap(body string) []string {
	var out []string
	for _, m := range locRe.FindAllStringSubmatch(body, -1) {
		out = append(out, strings.ReplaceAll(m[1], "&amp;", "&"))
	}
	return out
}

func brokenLinks(s *site) []Finding {
	base, err := url.Parse(s.home.url)
	if err != nil {
		return nil
	}
	from := map[string]string{} // адрес -> где на него ссылаются
	var order []string
	add := func(u, where string) {
		if _, ok := from[u]; !ok && len(order) < linkLimit && sameSite(u, base) != "" {
			from[u] = where
			order = append(order, u)
		}
	}
	for _, l := range s.page.Links {
		add(l, "главная")
	}
	// Шаг глубже: ссылки первых страниц главной.
	pages := par.Map(s.page.Links[:min(deepPages, len(s.page.Links))], 4, func(u string) []string {
		r := s.get(u, true)
		if r.status != 200 || !strings.Contains(strings.ToLower(r.header.Get("Content-Type")), "html") {
			return nil
		}
		pu, err := url.Parse(r.url)
		if err != nil {
			return nil
		}
		return ParsePage(r.body, pu).Links
	})
	for i, links := range pages {
		for _, l := range links {
			add(l, pathOf(s.page.Links[i]))
		}
	}
	if sm := s.get(s.base+"/sitemap.xml", true); sm.status == 200 {
		locs := ParseSitemap(sm.body)
		for _, l := range locs[:min(sitemapMax, len(locs))] {
			if !strings.HasSuffix(l, ".xml") {
				add(l, "sitemap")
			}
		}
	}
	if len(order) == 0 {
		return nil
	}
	statuses := par.Map(order, 8, func(u string) int { return s.get(u, true).status })
	var broken []string
	for i, st := range statuses {
		if st >= 400 || st == 0 {
			broken = append(broken, fmt.Sprintf("%s (%s, на %s)", pathOf(order[i]), statusWord(st), from[order[i]]))
		}
	}
	if len(broken) == 0 {
		return nil
	}
	return []Finding{{Area: "ссылки", Level: level.Warn, Text: fmt.Sprintf("битые ссылки: %d из %d", len(broken), len(order)),
		Why: "посетитель и поисковик упираются в ошибку: " + strings.Join(broken[:min(3, len(broken))], ", "),
		Fix: "поправить адреса или поставить редиректы; sitemap - только живые страницы"}}
}

func pathOf(u string) string {
	if p, err := url.Parse(u); err == nil && p.Path != "" {
		return p.Path
	}
	return u
}

func statusWord(st int) string {
	if st == 0 {
		return "не ответил"
	}
	return fmt.Sprint(st)
}
