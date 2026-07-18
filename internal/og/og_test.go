package og

import (
	"strings"
	"testing"
)

const page = `<!doctype html><html><head>
<title> Магазин коробок </title>
<meta name="description" content="Короба оптом">
<meta property="og:title" content="Короба от производителя">
<meta property="og:image" content="/img/share.jpg">
<meta name="twitter:description" content="Из twitter">
</head><body><meta property="og:description" content="не отсюда"></body></html>`

func TestParse(t *testing.T) {
	c := Parse(strings.NewReader(page))
	if c.Title != "Короба от производителя" || c.HTMLTitle != "Магазин коробок" || c.Image != "/img/share.jpg" {
		t.Fatalf("%+v", c)
	}
	// twitter:description подставляется, если og:description нет; meta из body не считается.
	if c.Description != "Из twitter" || c.MetaDescription != "Короба оптом" {
		t.Fatalf("описание: %+v", c)
	}
}
