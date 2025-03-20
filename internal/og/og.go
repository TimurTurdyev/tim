// Package og: как ссылка будет выглядеть в мессенджерах и соцсетях - разметка Open Graph и Twitter.
package og

import (
	"io"
	"net/url"
	"strings"

	"golang.org/x/net/html"
)

// Card: то, из чего мессенджер собирает превью.
type Card struct {
	Title, Description, Image, SiteName, Type, URL string
	TwitterCard                                    string
	HTMLTitle, MetaDescription                     string
}

// Parse читает head страницы. Тело дальше head не нужно: разметку превью ищут только там.
func Parse(r io.Reader) Card {
	var c Card
	z := html.NewTokenizer(r)
	inTitle := false
	for {
		switch z.Next() {
		case html.ErrorToken:
			return c
		case html.EndTagToken:
			name, _ := z.TagName()
			switch string(name) {
			case "head":
				return c
			case "title":
				inTitle = false
			}
		case html.TextToken:
			if inTitle && c.HTMLTitle == "" {
				c.HTMLTitle = strings.TrimSpace(string(z.Text()))
			}
		case html.StartTagToken, html.SelfClosingTagToken:
			name, hasAttr := z.TagName()
			switch string(name) {
			case "title":
				inTitle = true
			case "body":
				return c
			case "meta":
				if !hasAttr {
					continue
				}
				attrs := map[string]string{}
				for {
					k, v, more := z.TagAttr()
					attrs[strings.ToLower(string(k))] = strings.TrimSpace(string(v))
					if !more {
						break
					}
				}
				key := strings.ToLower(attrs["property"])
				if key == "" {
					key = strings.ToLower(attrs["name"])
				}
				set(&c, key, attrs["content"])
			}
		}
	}
}

func set(c *Card, key, v string) {
	dst := map[string]*string{
		"og:title": &c.Title, "og:description": &c.Description, "og:image": &c.Image,
		"og:site_name": &c.SiteName, "og:type": &c.Type, "og:url": &c.URL,
		"twitter:card": &c.TwitterCard, "description": &c.MetaDescription,
	}[key]
	if dst != nil && *dst == "" {
		*dst = v
	}
	// twitter:* - запасной вариант, если og нет.
	switch key {
	case "twitter:title":
		if c.Title == "" {
			c.Title = v
		}
	case "twitter:description":
		if c.Description == "" {
			c.Description = v
		}
	case "twitter:image", "twitter:image:src":
		if c.Image == "" {
			c.Image = v
		}
	}
}

// Problem: чего не хватает карточке.
type Problem struct {
	Bad  bool
	Text string
}

// Check: проблемы карточки без картинки; картинку проверяет вызывающий по ImageURL.
func Check(c Card) []Problem {
	var out []Problem
	if c.Title == "" {
		p := Problem{Bad: true, Text: "нет og:title - мессенджер возьмёт <title> или покажет голый адрес"}
		if c.HTMLTitle == "" {
			p.Text = "нет ни og:title, ни <title>"
		}
		out = append(out, p)
	}
	if c.Description == "" {
		out = append(out, Problem{Text: "нет og:description - под заголовком будет пусто или случайный текст"})
	}
	if c.Image == "" {
		out = append(out, Problem{Bad: true, Text: "нет og:image - превью без картинки"})
	} else if u, err := url.Parse(c.Image); err == nil && !u.IsAbs() {
		out = append(out, Problem{Text: "og:image относительный (" + c.Image + "): часть мессенджеров его не найдёт, нужен полный адрес"})
	}
	if c.Image != "" && c.TwitterCard == "" {
		out = append(out, Problem{Text: "нет twitter:card - в X картинка будет маленькой"})
	}
	return out
}

// ImageURL: адрес картинки относительно страницы.
func ImageURL(page string, c Card) string {
	base, err := url.Parse(page)
	if err != nil || c.Image == "" {
		return c.Image
	}
	ref, err := url.Parse(c.Image)
	if err != nil {
		return c.Image
	}
	return base.ResolveReference(ref).String()
}
