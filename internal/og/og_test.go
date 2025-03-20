package og

import (
	"strings"
	"testing"
)

func TestParseAndCheck(t *testing.T) {
	page := `<!doctype html><html><head>
<title> Магазин коробок </title>
<meta name="description" content="Короба оптом">
<meta property="og:title" content="Короба от производителя">
<meta property="og:image" content="/img/share.jpg">
<meta name="twitter:description" content="Из twitter">
</head><body><meta property="og:description" content="не отсюда"></body></html>`
	c := Parse(strings.NewReader(page))
	if c.Title != "Короба от производителя" || c.HTMLTitle != "Магазин коробок" || c.Image != "/img/share.jpg" {
		t.Fatalf("%+v", c)
	}
	// twitter:description подставляется, если og:description нет; meta из body не считается.
	if c.Description != "Из twitter" || c.MetaDescription != "Короба оптом" {
		t.Fatalf("описание: %+v", c)
	}
	probs := Check(c)
	var texts []string
	for _, p := range probs {
		texts = append(texts, p.Text)
	}
	all := strings.Join(texts, "|")
	if !strings.Contains(all, "относительный") || !strings.Contains(all, "twitter:card") || strings.Contains(all, "og:title") {
		t.Fatalf("%v", texts)
	}
	if got := ImageURL("https://shop.example.com/catalog/boxes", c); got != "https://shop.example.com/img/share.jpg" {
		t.Fatalf("адрес картинки %s", got)
	}
}

func TestEmpty(t *testing.T) {
	probs := Check(Parse(strings.NewReader("<html><head></head><body>x</body></html>")))
	bad := 0
	for _, p := range probs {
		if p.Bad {
			bad++
		}
	}
	if bad != 2 {
		t.Fatalf("%+v", probs)
	}
}
