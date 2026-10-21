package audit

import (
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"errors"
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

// checks: проверки аудита по порядку. Новая проверка - строка здесь: имя для "проверено" и правило от
// собранных фактов; сеть, если нужна, - внутри run. Проверки идут параллельно.
var checks = []check{
	{"заголовки", false, func(s *site) []Finding { return Headers(s.home.header, s.https) }},
	{"куки", false, func(s *site) []Finding { return Cookies(s.home.header.Values("Set-Cookie"), s.https) }},
	{"переход с http", true, func(s *site) []Finding { return one(plainHTTP(s.base)) }},
	{"TLS", true, func(s *site) []Finding { return TLSFindings(collectTLS(s.host, s.port), time.Now()) }},
	{"версии", false, func(s *site) []Finding {
		sw := ParseSoftware(s.home.header.Get("Server"), s.home.header.Get("X-Powered-By"), s.home.body)
		return VersionFindings(sw, releasesOf(sw))
	}},
	{"порты баз", false, func(s *site) []Finding { return PortFindings(openPorts(s.host)) }},
	{strconv.Itoa(len(ProbePaths())) + " путей", false, exposedFiles},
	{"страница ошибки", false, errorPage},
	{"индексация", false, indexing},
	{"www", false, canonicalHost},
	{"разметка", false, func(s *site) []Finding { return PageFindings(s.page) }},
	{"ссылки", false, brokenLinks},
	{"доставка", false, delivery},
	{"скорость", false, speed},
	{"dns", false, dns},
	{"поддомены", false, takeovers},
}

// check: одна проверка аудита; https - только для сайтов на https.
type check struct {
	name  string
	https bool
	run   func(s *site) []Finding
}

// site: что известно о сайте до проверок. insecure - главная открылась только без проверки сертификата:
// дальше ходим так же, а про сертификат скажет проверка TLS.
type site struct {
	target, base, host, port string
	https, insecure          bool
	home                     response
	page                     Page
}

// response: что ответил сайт на один запрос.
type response struct {
	url    string
	status int
	proto  string
	header http.Header
	body   string
	err    error
}

// Run проверяет сайт по адресу: главная, потом все проверки из таблицы.
func Run(target string) (*Report, error) {
	s, err := open(target)
	if err != nil {
		return nil, err
	}
	r := &Report{Target: target, Final: s.home.url, Status: s.home.status}
	if !s.https {
		r.Findings = append(r.Findings, Finding{Area: "https", Level: level.Bad, Text: "сайт открывается без https",
			Why: "пароли и куки идут открытым текстом, браузер помечает сайт как небезопасный",
			Fix: "сертификат Let's Encrypt (certbot --nginx) и 301 с http на https"})
	}
	if s.home.status >= 400 {
		r.Findings = append(r.Findings, Finding{Area: "https", Level: level.Info, Text: fmt.Sprintf("главная ответила %d", s.home.status),
			Why: "заголовки и куки ниже могут быть от заглушки, защиты от ботов или страницы обслуживания, а не от сайта",
			Fix: "проверить главную в браузере и повторить аудит, когда сайт отвечает 200"})
	}
	results := par.Map(checks, len(checks), func(c check) []Finding {
		if c.https && !s.https {
			return nil
		}
		return c.run(s)
	})
	for i, c := range checks {
		if c.https && !s.https {
			continue
		}
		r.Findings = append(r.Findings, results[i]...)
		r.Checked = append(r.Checked, c.name)
	}
	sortFindings(r.Findings)
	return r, nil
}

// open: главная по редиректам. Битый сертификат - не повод бросать аудит: главная открывается без
// проверки, а находку про сертификат даст проверка TLS.
func open(target string) (*site, error) {
	s := &site{target: target}
	s.home = s.get(target, true)
	var certErr *tls.CertificateVerificationError
	if errors.As(s.home.err, &certErr) {
		s.insecure = true
		s.home = s.get(target, true)
	}
	if s.home.err != nil {
		return nil, fmt.Errorf("главная не открылась: %w", s.home.err)
	}
	s.base, s.host, s.port = origin(s.home.url), hostOf(s.home.url), portOf(s.home.url)
	s.https = strings.HasPrefix(s.home.url, "https://")
	if u, err := url.Parse(s.home.url); err == nil {
		s.page = ParsePage(s.home.body, u)
	}
	return s, nil
}

// get: GET с ограничением тела; follow - идти по редиректам, иначе ответ на сам путь.
func (s *site) get(u string, follow ...bool) response {
	client := &http.Client{Timeout: timeout}
	if s.insecure {
		client.Transport = insecureTransport()
	}
	if len(follow) == 0 || !follow[0] {
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
	return response{url: resp.Request.URL.String(), status: resp.StatusCode, proto: resp.Proto, header: resp.Header, body: string(body)}
}

// insecureTransport: без проверки сертификата - он уже признан битым, идём смотреть остальное.
func insecureTransport() *http.Transport {
	return &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}} //nolint:gosec // осознанно, см. выше
}

// errorPage: несуществующий путь должен отвечать 404 без отладки; 200 на всё - "мягкая 404", поисковик
// индексирует мусор, а битые ссылки не видны.
func errorPage(s *site) []Finding {
	a := s.get(s.base + "/" + missingPath())
	out := one(DebugPage(a.body))
	if a.status == 200 {
		out = append(out, Finding{Area: "индексация", Level: level.Warn, Text: "несуществующая страница отвечает 200",
			Why: "поисковик считает мусорные адреса страницами, а битые ссылки не отличить от живых",
			Fix: "отдавать 404 с кодом, а не только с текстом; у SPA - на сервере, а не в роутере"})
	}
	return out
}

// exposedFiles: пути из списка параллельно; одна находка на вид файла, даже если открылись оба пути.
func exposedFiles(s *site) []Finding {
	paths := ProbePaths()
	answers := par.Map(paths, 8, func(p string) response { return s.get(s.base + p) })
	seen := map[string]bool{}
	var out []Finding
	for i, a := range answers {
		if f, ok := Exposed(paths[i], a.status, a.body); ok && !seen[f.Text] {
			seen[f.Text] = true
			out = append(out, f)
		}
	}
	return out
}

// plainHTTP: сайт по http отдаёт страницу, а не уводит на https.
func plainHTTP(base string) (Finding, bool) {
	a := (&site{}).get("http://" + strings.TrimPrefix(base, "https://") + "/")
	if a.err != nil || a.status != 200 {
		return Finding{}, false
	}
	return Finding{Area: "https", Level: level.Warn, Text: "по http сайт открывается без перехода на https",
		Why: "кто набрал адрес без https, работает по открытому каналу", Fix: "в nginx на 80-м порту: return 301 https://$host$request_uri;"}, true
}

func one(f Finding, ok bool) []Finding {
	if !ok {
		return nil
	}
	return []Finding{f}
}

// portOf: порт из адреса, без него - 443: аудит идёт только по https.
func portOf(u string) string {
	if p, err := url.Parse(u); err == nil && p.Port() != "" {
		return p.Port()
	}
	return "443"
}

func hostOf(u string) string {
	p, err := url.Parse(u)
	if err != nil {
		return ""
	}
	return p.Hostname()
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
