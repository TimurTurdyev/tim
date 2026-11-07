package audit

import (
	"maps"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/TimurTurdyev/tim/internal/level"
)

func TestHeaders(t *testing.T) {
	strong := http.Header{}
	strong.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
	strong.Set("Content-Security-Policy", "default-src 'self'; script-src 'self' 'nonce-abc' 'unsafe-inline'; frame-ancestors 'self'")
	strong.Set("X-Content-Type-Options", "nosniff")
	if got := Headers(strong, true); len(got) != 0 {
		t.Errorf("строгие заголовки: %+v", got)
	}
	weak := http.Header{}
	weak.Set("Strict-Transport-Security", "max-age=300")
	weak.Set("Content-Security-Policy", "default-src 'self' 'unsafe-inline'")
	want := map[string]level.Level{"HSTS короче полугода": level.Info, "CSP разрешает inline-скрипты": level.Info,
		"сайт можно встроить во фрейм": level.Warn, "нет X-Content-Type-Options": level.Info}
	got := map[string]level.Level{}
	for _, f := range Headers(weak, true) {
		got[f.Text] = f.Level
	}
	if len(got) != len(want) {
		t.Fatalf("слабые заголовки: %v", got)
	}
	for k, l := range want {
		if got[k] != l {
			t.Errorf("%s: %v, ждали %v", k, got[k], l)
		}
	}
	// По http HSTS не спрашивается: браузер его там не примет.
	for _, f := range Headers(http.Header{}, false) {
		if f.Text == "нет HSTS" {
			t.Error("HSTS на http")
		}
	}
}

func TestCookies(t *testing.T) {
	got := Cookies([]string{
		"shop_session=abc; path=/; secure; samesite=lax",
		"XSRF-TOKEN=x; path=/; secure",
		"PHPSESSID=y; path=/; HttpOnly",
		"_ga=z; path=/",
	}, true)
	if len(got) != 2 || got[0].Level != level.Bad || got[0].Text != "кука сессии доступна скриптам: shop_session" ||
		got[1].Text != "куки без Secure: PHPSESSID, _ga" {
		t.Fatalf("%+v", got)
	}
}

func TestExposedBySignature(t *testing.T) {
	cases := []struct {
		path   string
		status int
		body   string
		want   bool
	}{
		{"/.env", 200, "APP_NAME=Shop\nAPP_KEY=base64:xyz\n", true},
		{"/.env", 200, "<!doctype html><title>Shop</title>", false}, // заглушка отдаёт главную на любой путь
		{"/.env", 403, "APP_KEY=base64:xyz", false},
		{"/.git/HEAD", 200, "ref: refs/heads/main\n", true},
		{"/storage/logs/laravel.log", 200, "[2026-10-27 10:00:01] production.ERROR: SQLSTATE", true},
		{"/backup.sql", 200, "-- MySQL dump 10.13\nCREATE TABLE users", true},
		{"/_ignition/health-check", 200, `{"can_execute_commands":true}`, true},
		{"/composer.json", 200, `{"require": {"php": "^8.2"}}`, true},
	}
	for _, c := range cases {
		if _, ok := Exposed(c.path, c.status, c.body); ok != c.want {
			t.Errorf("%s %d: %v, ждали %v", c.path, c.status, ok, c.want)
		}
	}
	if _, ok := DebugPage("<h1>Whoops! There was an error.</h1>"); !ok {
		t.Error("Whoops не узнан")
	}
	if _, ok := DebugPage("<h1>404 Not Found</h1>"); ok {
		t.Error("обычная 404 - не отладка")
	}
}

func TestVersionFindings(t *testing.T) {
	releases, err := ParseReleases([]byte(`{"result":{"releases":[
		{"name":"8.4","isEol":false,"eolFrom":"2028-12-31"},
		{"name":"7.4","isEol":true,"eolFrom":"2022-11-28"}]}}`))
	if err != nil || len(releases) != 2 {
		t.Fatalf("%v %v", releases, err)
	}
	sw := ParseSoftware("nginx/1.18.0", "PHP/7.4.33", `<meta name="generator" content="WordPress 6.4.2">`)
	if len(sw) != 3 || sw[0].Distro || sw[1].Distro || sw[2].Header {
		t.Fatalf("%+v", sw)
	}
	// Сервер из пакетов Ubuntu - и PHP рядом считается пакетом дистрибутива.
	if ubuntu := ParseSoftware("nginx/1.18.0 (Ubuntu)", "PHP/7.4.33", ""); !ubuntu[1].Distro {
		t.Errorf("%+v", ubuntu)
	}
	got := VersionFindings(sw, map[string][]Release{"php": releases})
	// PHP 7.4 с php.net - плохо; версии в заголовках - к сведению
	if len(got) != 2 || got[0].Level != level.Bad || !strings.HasPrefix(got[0].Text, "php 7.4 ") || got[1].Level != level.Info {
		t.Fatalf("%+v", got)
	}
}

func TestParseRobotsAndPage(t *testing.T) {
	closed, maps := ParseRobots("User-agent: *\nDisallow: /\nSitemap: https://example.com/sitemap.xml\n")
	if !closed || len(maps) != 1 {
		t.Errorf("robots: %v %v", closed, maps)
	}
	// Запрет для одного робота - сайт не закрыт.
	if closed, _ := ParseRobots("User-agent: Yandex\nDisallow: /\n\nUser-agent: *\nDisallow: /admin\n"); closed {
		t.Error("закрыт только для Yandex")
	}
	base, _ := url.Parse("https://example.com/")
	p := ParsePage(`<html lang="ru"><head><title>Shop</title><link rel="icon" href="/favicon.ico">
<link rel="stylesheet" href="/app.css"><script src="http://cdn.example.net/a.js"></script></head>
<body><h1>Hi</h1><a href="/about">about</a><a href="/about#top">dup</a><a href="mailto:a@b.c">m</a>
<a href="https://other.example.org/x">ext</a><img src="http://example.com/i.png"></body></html>`, base)
	if p.Title != "Shop" || p.Lang != "ru" || !p.Favicon || p.H1 != 1 || len(p.Mixed) != 2 ||
		len(p.Links) != 1 || p.Links[0] != "https://example.com/about" || len(p.Assets) != 1 {
		t.Errorf("%+v", p)
	}
}

// Порт базы считается открытым только по ответу самого сервиса: за VPN соединение принимает кто угодно.
func TestIdentify(t *testing.T) {
	yes := map[int]string{
		3306:  "J\x00\x00\x00\x0a8.0.36\x00",
		5432:  "N",
		6379:  "-NOAUTH Authentication required.\r\n",
		27017: "HTTP/1.0 200 OK\r\n\r\nIt looks like you are trying to access MongoDB over HTTP on the native driver port.\r\n",
		9200:  "HTTP/1.0 401 Unauthorized\r\nX-elastic-product: Elasticsearch\r\n\r\n",
		11211: "VERSION 1.6.21\r\n",
		5984:  "HTTP/1.0 200 OK\r\n\r\n{\"couchdb\":\"Welcome\",\"version\":\"3.3.3\"}",
		8086:  "HTTP/1.0 204 No Content\r\nX-Influxdb-Version: 1.8.10\r\n\r\n",
		2375:  "HTTP/1.0 200 OK\r\n\r\n{\"Platform\":{\"Name\":\"Docker Engine\"},\"ApiVersion\":\"1.45\"}",
	}
	for port, reply := range yes {
		if !Identify(port, []byte(reply)) {
			t.Errorf("%d: ответ сервиса не узнан: %q", port, reply)
		}
	}
	nginx := "HTTP/1.1 400 Bad Request\r\nServer: nginx\r\n\r\n<html><head><title>400 Bad Request</title></head></html>"
	for port := range DBPorts {
		if Identify(port, nil) || Identify(port, []byte(nginx)) {
			t.Errorf("%d: пустой или чужой ответ принят за сервис", port)
		}
	}
}

// Признаки - из стандартных шапок OpenCart 1.5.6.4, 2.3.0.2, 3.0.x и 4.0.2.3 и подвала входа в админку.
func TestOpenCart(t *testing.T) {
	heads := map[string]string{
		`<script src="catalog/view/javascript/jquery/jquery-1.7.1.min.js"></script>`: "1.5",
		`<script src="catalog/view/javascript/jquery/jquery-2.1.1.min.js"></script>`: "2-3",
		`<script src="catalog/view/javascript/jquery/jquery-3.7.1.min.js"></script>
<link href="catalog/view/javascript/bootstrap/css/bootstrap.min.css" rel="stylesheet">`: "3",
		`<link href="catalog/view/stylesheet/bootstrap.css"><script src="catalog/view/javascript/jquery/jquery-3.7.1.min.js">`: "4",
		`<a href="https://shop.example/index.php?route=account/login&amp;language=ru-ru">`:                                     "4",
		`<link href="catalog/view/theme/custom/stylesheet/stylesheet.css">`:                                                    "",
	}
	for html, want := range heads {
		if got := OpenCartBranch(html); got != want {
			t.Errorf("%q: ветка %q, а не %q", html, got, want)
		}
	}
	admin := `<footer id="footer"><a href="https://www.opencart.com">OpenCart</a> &copy; 2009-2026 All Rights Reserved.<br/>Version 3.0.3.8</footer>`
	if v := OpenCartVersion(admin); v != "3.0.3.8" {
		t.Errorf("версия из подвала: %q", v)
	}
	levels := map[[2]string]level.Level{{"2-3", ""}: level.Info, {"2-3", "2.3.0.2"}: level.Bad, {"1.5", ""}: level.Bad,
		{"", "1.5.6.4"}: level.Bad, {"2-3", "3.0.3.8"}: level.Info, {"4", ""}: level.Info}
	for in, want := range levels {
		f := OpenCartFindings(in[0], in[1])
		if len(f) != 1 || f[0].Level != want {
			t.Errorf("%v: %+v", in, f)
		}
	}
}

func TestForeignLogins(t *testing.T) {
	page := `<script src="https://accounts.google.com/gsi/client" async></script>
<script src="https://connect.facebook.net/en_US/fbevents.js"></script>
<script src="https://telegram.org/js/telegram-widget.js?22" data-telegram-login="shop_bot"></script>`
	found := ForeignLogins(page)
	if !slices.Equal(found, []string{"Google", "Telegram"}) {
		t.Fatalf("пиксель Facebook - не вход: %v", found)
	}
	if !Russian("shop.ru", "") || !Russian("example.com", "ru-RU") || Russian("example.com", "en") {
		t.Error("российский сайт - по зоне или языку страницы")
	}
	got := map[string]level.Level{}
	for _, f := range LoginFindings(append(found, "uLogin"), true) {
		got[f.Text] = f.Level
	}
	want := map[string]level.Level{"вход через Google": level.Bad, "вход через Telegram": level.Info, "вход через виджет uLogin": level.Warn}
	if !maps.Equal(got, want) {
		t.Errorf("находки: %v", got)
	}
	if f := LoginFindings(found, false); len(f) != 0 {
		t.Errorf("не российский сайт: %+v", f)
	}
}
