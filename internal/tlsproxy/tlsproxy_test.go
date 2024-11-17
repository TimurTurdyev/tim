package tlsproxy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestHandlerKeepsHostAndScheme(t *testing.T) {
	var got http.Header
	var host string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, host = r.Header.Clone(), r.Host
		_, _ = io.WriteString(w, "ok")
	}))
	defer backend.Close()
	target, _ := url.Parse(backend.URL)
	front := httptest.NewTLSServer(Handler(target))
	defer front.Close()

	req, _ := http.NewRequest("GET", front.URL+"/path", nil)
	req.Host = "shop.test:8443"
	resp, err := front.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("статус %d", resp.StatusCode)
	}
	// Приложение должно видеть своё имя, а не 127.0.0.1:порт бэкенда - иначе сессии и ссылки поедут.
	if host != "shop.test:8443" {
		t.Errorf("Host дошёл как %q", host)
	}
	if got.Get("X-Forwarded-Proto") != "https" || got.Get("X-Forwarded-Port") != "8443" {
		t.Errorf("заголовки: proto=%q port=%q", got.Get("X-Forwarded-Proto"), got.Get("X-Forwarded-Port"))
	}
}

func TestBackendDown(t *testing.T) {
	target, _ := url.Parse("http://127.0.0.1:1")
	front := httptest.NewTLSServer(Handler(target))
	defer front.Close()
	resp, err := front.Client().Get(front.URL)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("статус %d, ожидали 502", resp.StatusCode)
	}
}
