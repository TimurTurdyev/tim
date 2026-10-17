package audit

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/TimurTurdyev/tim/internal/level"
	"github.com/TimurTurdyev/tim/internal/par"
)

const (
	timeout   = 8 * time.Second
	bodyLimit = 256 << 10
	userAgent = "Mozilla/5.0 (Macintosh) tim-audit"
)

// response: что ответил сайт на один запрос.
type response struct {
	url    string
	status int
	header http.Header
	body   string
	err    error
}

// Run проверяет сайт по адресу: главная, переход с http, пути из списка и страница ошибки.
func Run(target string) (*Report, error) {
	home := fetch(target, true)
	if home.err != nil {
		return nil, fmt.Errorf("главная не открылась: %w", home.err)
	}
	r := &Report{Target: target, Final: home.url, Status: home.status}
	https := strings.HasPrefix(home.url, "https://")
	if !https {
		r.Findings = append(r.Findings, Finding{Area: "https", Level: level.Bad, Text: "сайт открывается без https",
			Why: "пароли и куки идут открытым текстом, браузер помечает сайт как небезопасный",
			Fix: "сертификат Let's Encrypt (certbot --nginx) и 301 с http на https"})
	}
	r.Findings = append(r.Findings, Headers(home.header, https)...)
	r.Findings = append(r.Findings, Cookies(home.header.Values("Set-Cookie"), https)...)
	r.Checked = append(r.Checked, "заголовки", "куки")
	base := origin(home.url)
	if https {
		if f, ok := plainHTTP(base); ok {
			r.Findings = append(r.Findings, f)
		}
		r.Checked = append(r.Checked, "переход с http")
	}
	paths := ProbePaths()
	answers := par.Map(paths, 8, func(p string) response { return fetch(base+p, false) })
	seen := map[string]bool{}
	for i, a := range answers {
		f, ok := Exposed(paths[i], a.status, a.body)
		if ok && !seen[f.Text] {
			seen[f.Text] = true
			r.Findings = append(r.Findings, f)
		}
	}
	r.Checked = append(r.Checked, strconv.Itoa(len(paths))+" путей")
	if f, ok := DebugPage(fetch(base+"/"+missingPath(), false).body); ok {
		r.Findings = append(r.Findings, f)
	}
	r.Checked = append(r.Checked, "страница ошибки")
	sortFindings(r.Findings)
	return r, nil
}

// fetch: GET с ограничением тела; follow - идти по редиректам, иначе ответ на сам путь.
func fetch(u string, follow bool) response {
	client := &http.Client{Timeout: timeout}
	if !follow {
		client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	}
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return response{err: err}
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := client.Do(req)
	if err != nil {
		return response{err: err}
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, bodyLimit))
	return response{url: resp.Request.URL.String(), status: resp.StatusCode, header: resp.Header, body: string(body)}
}

// plainHTTP: сайт по http отдаёт страницу, а не уводит на https.
func plainHTTP(base string) (Finding, bool) {
	a := fetch("http://"+strings.TrimPrefix(base, "https://")+"/", false)
	if a.err != nil || a.status != 200 {
		return Finding{}, false
	}
	return Finding{Area: "https", Level: level.Warn, Text: "по http сайт открывается без перехода на https",
		Why: "кто набрал адрес без https, работает по открытому каналу", Fix: "в nginx на 80-м порту: return 301 https://$host$request_uri;"}, true
}

func origin(u string) string {
	p, err := url.Parse(u)
	if err != nil {
		return strings.TrimRight(u, "/")
	}
	return p.Scheme + "://" + p.Host
}

// missingPath: заведомо несуществующий путь - чтобы увидеть страницу ошибки, а не чью-то страницу.
func missingPath() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return "tim-audit-" + hex.EncodeToString(b)
}
