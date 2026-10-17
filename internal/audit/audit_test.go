package audit

import (
	"net/http"
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
