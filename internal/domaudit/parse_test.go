package domaudit

import (
	"slices"
	"testing"
)

const crtSample = `[
 {"common_name":"example.com","name_value":"*.example.com\nexample.com"},
 {"common_name":"api.example.com","name_value":"api.example.com\nold.api.example.com"},
 {"common_name":"shop.example.net","name_value":"shop.example.net\nexample.net\nwww.example.co.uk"},
 {"common_name":"example.com","name_value":"api.example.com"},
 {"common_name":"traefik.default","name_value":"traefik.default\nrouter.lan\npages.example.github.io"}
]`

func TestParseCRT(t *testing.T) {
	subs, related, err := ParseCRT([]byte(crtSample), "example.com")
	if err != nil {
		t.Fatal(err)
	}
	wantSubs := []string{"api.example.com", "example.com", "old.api.example.com"}
	if len(subs) != len(wantSubs) {
		t.Fatalf("subs %v", subs)
	}
	for i := range wantSubs {
		if subs[i] != wantSubs[i] {
			t.Fatalf("subs %v want %v", subs, wantSubs)
		}
	}
	// Имена из выдуманных зон (сертификат по умолчанию, домашняя сеть) связанными доменами не считаются.
	wantRel := []string{"example.co.uk", "example.github.io", "example.net"}
	if !slices.Equal(related, wantRel) {
		t.Errorf("related %v want %v", related, wantRel)
	}
	if _, _, err := ParseCRT([]byte("not json"), "example.com"); err == nil {
		t.Error("broken json should fail")
	}
}

func TestParseHostSearch(t *testing.T) {
	subs, rows, err := ParseHostSearch("admin.example.com,192.0.2.4\nwww.example.com,192.0.2.4\nother.org,192.0.2.8\n", "example.com")
	if err != nil || rows != 3 || len(subs) != 2 || subs[0] != "admin.example.com" {
		t.Fatalf("%v %d %v", subs, rows, err)
	}
	if _, _, err := ParseHostSearch("error API count exceeded\n", "example.com"); err == nil {
		t.Error("quota error should be reported")
	}
}

func TestParseTHC(t *testing.T) {
	subs, rows := ParseTHC("Example.com\nwww.example.com\nwww.example.com\n\nexample.com.evil.org\n", "example.com")
	if rows != 4 || !slices.Equal(subs, []string{"example.com", "www.example.com"}) {
		t.Errorf("%v, строк %d", subs, rows)
	}
	if subs, rows := ParseTHC("", "example.com"); subs != nil || rows != 0 {
		t.Errorf("пустой ответ: %v %d", subs, rows)
	}
}

func TestParseSubdomainCenter(t *testing.T) {
	subs, err := ParseSubdomainCenter([]byte(`["b.example.com", "a.example.com", "example.net", "*.c.example.com"]`), "example.com")
	if err != nil || !slices.Equal(subs, []string{"a.example.com", "b.example.com", "c.example.com"}) {
		t.Errorf("%v %v", subs, err)
	}
	if _, err := ParseSubdomainCenter([]byte(`{"error": "rate limit"}`), "example.com"); err == nil {
		t.Error("объект вместо массива принят")
	}
}

func TestMatchTakeover(t *testing.T) {
	cases := []struct {
		target   string
		resolves bool
		body     string
		want     string // сервис; пусто - захвата нет
	}{
		{"user.github.io", true, "<h1>There isn't a GitHub Pages site here.</h1>", "GitHub Pages"},
		{"user.github.io", true, "<h1>My project</h1>", ""},
		{"app-1234.herokuapp.com", false, "", "Heroku"},
		{"shop.azurewebsites.net", false, "", "Azure"},
		{"shop.azurewebsites.net", true, "<html>ok</html>", ""},
		{"cdn.example-hosting.com", false, "", ""},
		{"", false, "", ""},
	}
	for _, c := range cases {
		got, ok := MatchTakeover(c.target, c.resolves, c.body)
		if ok != (c.want != "") || got.Service != c.want {
			t.Errorf("%s resolves=%v: %+v %v, ждали %q", c.target, c.resolves, got, ok, c.want)
		}
	}
}

// Заголовок страницы и имена пишут чужие люди: управляющие последовательности терминала дальше разбора не идут.
func TestForeignText(t *testing.T) {
	if got := Title("<title>ok &#27;[2J&#27;]0;pwn&#7; x</title>"); got != "ok [2J ]0;pwn x" {
		t.Errorf("заголовок: %q", got)
	}
	names := []string{"a\x1b[31m.example.com", "b.example.com:8443", "c.example.com/x", "_dmarc.example.com", "*.d.example.com",
		"e..example.com", "example.org:8443", "example.org/x", "192.0.2.1", "www.example.net"}
	subs, related := splitNames(names, "example.com")
	if want := []string{"_dmarc.example.com", "d.example.com"}; !slices.Equal(subs, want) {
		t.Errorf("поддомены %q, ждали %q", subs, want)
	}
	if want := []string{"example.net"}; !slices.Equal(related, want) {
		t.Errorf("связанные %q, ждали %q", related, want)
	}
}

func TestParseTHCNeighbors(t *testing.T) {
	n, err := ParseTHCNeighbors([]byte(`{"matching_records":75,"domains":[{"domain":"A.example.com"},{"domain":"b\u001b.example"},{"domain":"shop.example.net"}]}`))
	if err != nil || n.Seen != 75 || !slices.Equal(n.Names, []string{"a.example.com", "shop.example.net"}) {
		t.Errorf("%+v %v", n, err)
	}
	if _, err := ParseTHCNeighbors([]byte("rate limit")); err == nil {
		t.Error("не JSON принят")
	}
	names := []string{"a.one.com", "b.one.com", "one.com", "two.org", "www.two.org", "example.com", "www.example.com", "192-0-2-1.host.net"}
	if got := onePerSite(foreign(names, "192.0.2.1", "api.example.com"), 3); !slices.Equal(got, []string{"one.com", "two.org", "a.one.com"}) {
		t.Errorf("соседи: %v", got)
	}
}

func TestDecode(t *testing.T) {
	cp1251 := []byte("<title>\xcf\xf0\xe8\xe2\xe5\xf2</title>")
	cases := []struct {
		body        []byte
		contentType string
		want        string
	}{
		{cp1251, "text/html; charset=Windows-1251", "Привет"},
		{append([]byte(`<meta http-equiv="Content-Type" content="text/html; charset=cp1251">`), cp1251...), "text/html", "Привет"},
		{[]byte("<title>\xf0\xd2\xc9\xd7\xc5\xd4</title>"), `text/html; charset="koi8-r"`, "Привет"},
		{[]byte("<title>Привет</title>"), "text/html; charset=windows-1251", "Привет"}, // заголовок врёт: это уже UTF-8
		{cp1251, "text/html", "?"},
	}
	for _, c := range cases {
		if got := Title(decode(c.body, c.contentType)); got != c.want {
			t.Errorf("%s: %q, ждали %q", c.contentType, got, c.want)
		}
	}
}
