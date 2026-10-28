package audit

import (
	"fmt"
	"net/url"
	"strings"

	"golang.org/x/net/html"

	"github.com/TimurTurdyev/tim/internal/level"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// maxLinks: сколько внутренних ссылок главной проверять на битость.
const maxLinks = 50

// Page: что видно в разметке главной.
type Page struct {
	Title, Description, Lang, Canonical string
	H1                                  int
	Favicon                             bool
	Mixed                               []string // ресурсы по http на странице https
	Links                               []string // внутренние ссылки, абсолютные, без повторов
	Assets                              []string // css и js с этого же сайта
}

// ParsePage разбирает главную: заголовки и описание, смешанное содержимое, ссылки и статику.
func ParsePage(body string, base *url.URL) Page {
	var p Page
	doc, err := html.Parse(strings.NewReader(body))
	if err != nil {
		return p
	}
	seen := map[string]bool{}
	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			p.element(n, base, seen)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	return p
}

// element: один тег - что он добавляет к странице.
func (p *Page) element(n *html.Node, base *url.URL, seen map[string]bool) {
	attr := func(name string) string {
		for _, a := range n.Attr {
			if strings.EqualFold(a.Key, name) {
				return strings.TrimSpace(a.Val)
			}
		}
		return ""
	}
	switch n.Data {
	case "html":
		p.Lang = attr("lang")
	case "title":
		if n.FirstChild != nil && p.Title == "" {
			p.Title = strings.TrimSpace(n.FirstChild.Data)
		}
	case "meta":
		if strings.EqualFold(attr("name"), "description") {
			p.Description = attr("content")
		}
	case "h1":
		p.H1++
	case "link":
		rel := strings.ToLower(attr("rel"))
		href := attr("href")
		switch {
		case strings.Contains(rel, "icon"):
			p.Favicon = true
		case rel == "canonical":
			p.Canonical = href
		case rel == "stylesheet":
			p.asset(href, base)
		}
		p.mixed(href, base)
	case "script", "img", "iframe", "video", "audio", "source":
		src := attr("src")
		p.mixed(src, base)
		if n.Data == "script" {
			p.asset(src, base)
		}
	case "a":
		p.link(attr("href"), base, seen)
	}
}

// mixed: ресурс по http на странице https браузер блокирует или помечает.
func (p *Page) mixed(ref string, base *url.URL) {
	if base.Scheme == "https" && strings.HasPrefix(strings.ToLower(ref), "http://") {
		p.Mixed = append(p.Mixed, ref)
	}
}

func (p *Page) asset(ref string, base *url.URL) {
	if u := sameSite(ref, base); u != "" && len(p.Assets) < 5 {
		p.Assets = append(p.Assets, u)
	}
}

func (p *Page) link(ref string, base *url.URL, seen map[string]bool) {
	u := sameSite(ref, base)
	if u == "" || seen[u] || u == base.String() || len(p.Links) >= maxLinks {
		return
	}
	seen[u] = true
	p.Links = append(p.Links, u)
}

// sameSite: абсолютный адрес, если ссылка ведёт на этот же сайт и это страница, а не почта или якорь.
func sameSite(ref string, base *url.URL) string {
	if ref == "" || strings.HasPrefix(ref, "#") || strings.HasPrefix(ref, "mailto:") || strings.HasPrefix(ref, "tel:") || strings.HasPrefix(ref, "javascript:") {
		return ""
	}
	u, err := base.Parse(ref)
	if err != nil || u.Host != base.Host || (u.Scheme != "http" && u.Scheme != "https") {
		return ""
	}
	u.Fragment = ""
	return u.String()
}

// PageFindings: чего не хватает в разметке и что сломано.
func PageFindings(p Page) []Finding {
	var out []Finding
	add := func(l level.Level, text, why, fix string) {
		out = append(out, Finding{Area: "страница", Level: l, Text: text, Why: why, Fix: fix})
	}
	if p.Title == "" {
		add(level.Warn, "нет title", "в поиске и во вкладке сайт без имени", "<title> с названием и сутью страницы")
	}
	if p.Description == "" {
		add(level.Info, "нет meta description", "поисковик сам выберет кусок текста для сниппета", `<meta name="description" content="...">`)
	}
	if p.Lang == "" {
		add(level.Info, "нет lang у html", "браузер и читалки не знают язык страницы, переводчик предлагается невпопад", `<html lang="ru">`)
	}
	if p.H1 == 0 {
		add(level.Info, "нет h1", "поисковику и читалкам не за что зацепиться", "один <h1> с темой страницы")
	}
	if !p.Favicon {
		add(level.Info, "нет favicon", "вкладка без значка, в закладках сайт не узнать", `<link rel="icon" href="/favicon.ico">`)
	}
	if len(p.Mixed) > 0 {
		add(level.Warn, fmt.Sprintf("смешанное содержимое: %d %s по http", len(p.Mixed), ui.Plural(len(p.Mixed), "ресурс", "ресурса", "ресурсов")),
			"браузер блокирует скрипты по http и снимает замок у страницы: "+strings.Join(p.Mixed[:min(3, len(p.Mixed))], ", "),
			"ссылки на ресурсы без схемы (//host/...) или https://")
	}
	return out
}
