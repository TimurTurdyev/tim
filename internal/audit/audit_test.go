package audit

import (
	"net/http"
	"strings"
	"testing"
	"time"

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

func TestTLSFindings(t *testing.T) {
	now := time.Date(2026, 10, 27, 0, 0, 0, 0, time.UTC)
	got := TLSFindings(TLSFacts{Old: []string{"TLS 1.0"}, NoChain: true, RSABits: 1024, Expires: now.AddDate(0, 0, 5)}, now)
	levels := map[level.Level]int{}
	for _, f := range got {
		levels[f.Level]++
	}
	// ключ 1024 и срок 5 дней - плохо; старая версия и нет цепочки - внимание
	if levels[level.Bad] != 2 || levels[level.Warn] != 2 {
		t.Fatalf("%+v", got)
	}
	if got := TLSFindings(TLSFacts{RSABits: 2048, Expires: now.AddDate(0, 2, 0)}, now); len(got) != 0 {
		t.Errorf("здоровый TLS: %+v", got)
	}
}

func TestVersionFindings(t *testing.T) {
	releases, err := ParseReleases([]byte(`{"result":{"releases":[
		{"name":"8.4","isEol":false,"eolFrom":"2028-12-31"},
		{"name":"7.4","isEol":true,"eolFrom":"2022-11-28"}]}}`))
	if err != nil || len(releases) != 2 {
		t.Fatalf("%v %v", releases, err)
	}
	sw := ParseSoftware("nginx/1.18.0 (Ubuntu)", "PHP/7.4.33", `<meta name="generator" content="WordPress 6.4.2">`)
	if len(sw) != 3 || !sw[0].Distro || sw[1].Distro || sw[2].Header {
		t.Fatalf("%+v", sw)
	}
	got := VersionFindings(sw, map[string][]Release{"php": releases, "nginx": {{Name: "1.18", IsEol: true, EolFrom: "2021-04-20"}}})
	// PHP 7.4 с php.net - плохо; nginx из пакетов Ubuntu - срок не сверяется; версии в заголовках - к сведению
	if len(got) != 2 || got[0].Level != level.Bad || !strings.HasPrefix(got[0].Text, "php 7.4 ") || got[1].Level != level.Info {
		t.Fatalf("%+v", got)
	}
}
