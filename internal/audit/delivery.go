package audit

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/dns/dnsmessage"

	"github.com/TimurTurdyev/tim/internal/dnsq"
	"github.com/TimurTurdyev/tim/internal/inspect"
	"github.com/TimurTurdyev/tim/internal/level"
)

// Пороги скорости: первый байт дольше 400 мс заметен, дольше 800 - сайт "тормозит".
const (
	ttfbNotice = 400 * time.Millisecond
	ttfbSlow   = 800 * time.Millisecond
	cacheMin   = 24 * time.Hour
)

var maxAgeCacheRe = regexp.MustCompile(`max-age=(\d+)`)

// delivery: сжатие, HTTP/2 и кэш статики - то, что делает сайт быстрым без правок кода.
func delivery(s *site) []Finding {
	var out []Finding
	add := func(l level.Level, text, why, fix string) {
		out = append(out, Finding{Area: "доставка", Level: l, Text: text, Why: why, Fix: fix})
	}
	if enc := encodingOf(s); enc == "" {
		add(level.Warn, "страницы без сжатия", "HTML, CSS и JS едут в 3-5 раз тяжелее, чем могли бы",
			"в nginx: gzip on; gzip_types text/css application/javascript application/json image/svg+xml; лучше brotli")
	}
	if s.https && !strings.HasPrefix(s.home.proto, "HTTP/2") && !strings.HasPrefix(s.home.proto, "HTTP/3") {
		add(level.Info, "нет HTTP/2", "каждый файл ждёт своей очереди в соединении, страница собирается дольше", "в nginx: listen 443 ssl http2;")
	}
	if len(s.page.Assets) > 0 {
		a := s.get(s.page.Assets[0])
		if a.err == nil && a.status == 200 && cacheAge(a.header) < cacheMin {
			add(level.Info, "статика без долгого кэша", "css и js скачиваются заново при каждом заходе: "+s.page.Assets[0],
				"Cache-Control: max-age=31536000, immutable для файлов с хэшем в имени")
		}
	}
	return out
}

// encodingOf: чем сервер сжимает, если попросить как браузер. Сам клиент Go распаковывает gzip молча,
// поэтому запрос отдельный, с явным Accept-Encoding.
func encodingOf(s *site) string {
	client := &http.Client{Timeout: timeout}
	if s.insecure {
		client.Transport = insecureTransport()
	}
	req, err := http.NewRequest(http.MethodGet, s.home.url, nil)
	if err != nil {
		return ""
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept-Encoding", "br, gzip, zstd")
	resp, err := client.Do(req)
	if err != nil {
		return ""
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, bodyLimit))
	return resp.Header.Get("Content-Encoding")
}

func cacheAge(h http.Header) time.Duration {
	m := maxAgeCacheRe.FindStringSubmatch(h.Get("Cache-Control"))
	if m == nil {
		return 0
	}
	n, _ := strconv.Atoi(m[1])
	return time.Duration(n) * time.Second
}

// speed: первый байт и размер главной - по одному запросу на свежем соединении.
func speed(s *site) []Finding {
	t, err := inspect.Measure(s.home.url)
	if err != nil {
		return nil
	}
	var out []Finding
	switch {
	case t.TTFB > ttfbSlow:
		out = append(out, Finding{Area: "скорость", Level: level.Warn, Text: fmt.Sprintf("первый байт за %d мс", t.TTFB.Milliseconds()),
			Why: "сервер думает дольше, чем грузится вся страница у быстрых сайтов: база, кэш или PHP без opcache",
			Fix: "кэш страниц, opcache, индексы в базе; tim http <url> покажет фазы"})
	case t.TTFB > ttfbNotice:
		out = append(out, Finding{Area: "скорость", Level: level.Info, Text: fmt.Sprintf("первый байт за %d мс", t.TTFB.Milliseconds()),
			Why: "заметно, но терпимо", Fix: "кэш страниц или opcache"})
	}
	if t.Size > 2<<20 {
		out = append(out, Finding{Area: "скорость", Level: level.Info, Text: fmt.Sprintf("главная весит %.1f MB", float64(t.Size)/(1<<20)),
			Why: "на мобильном сети страница открывается секунды", Fix: "картинки в webp и lazy loading, лишний JS"})
	}
	return out
}

// dns: CAA, DNSSEC и разнесены ли NS - то, что видно до первого байта сайта.
func dns(s *site) []Finding {
	if isIP(s.host) || !strings.Contains(s.host, ".") {
		return nil
	}
	zone := inspect.Registrable(s.host)
	var out []Finding
	add := func(l level.Level, text, why, fix string) {
		out = append(out, Finding{Area: "dns", Level: l, Text: text, Why: why, Fix: fix})
	}
	if caa, err := dnsq.CAA(zone); err == nil && len(caa) == 0 {
		add(level.Info, "нет записи CAA", "сертификат на домен может выпустить любой центр", "CAA 0 issue \"letsencrypt.org\" у регистратора")
	}
	if signed, err := dnsq.Signed(zone); err == nil && !signed {
		add(level.Info, "зона без DNSSEC", "ответы DNS можно подменить по дороге; для большинства сайтов это терпимо", "включить DNSSEC у регистратора, если он умеет")
	}
	if nets := nsNetworks(zone); len(nets) == 1 && nets[0] != "" {
		add(level.Info, "все NS в одной сети "+nets[0], "упала сеть - упал весь домен, включая почту", "второй NS у другого провайдера")
	}
	return out
}

// nsNetworks: сети /24, в которых живут NS зоны.
func nsNetworks(zone string) []string {
	a, err := dnsq.Query(zone, dnsmessage.TypeNS)
	if err != nil {
		return nil
	}
	var nets []string
	for _, r := range a.Records {
		ns, ok := r.Body.(*dnsmessage.NSResource)
		if !ok {
			continue
		}
		ips, err := net.LookupIP(strings.TrimSuffix(ns.NS.String(), "."))
		if err != nil {
			continue
		}
		for _, ip := range ips {
			if v4 := ip.To4(); v4 != nil {
				n := fmt.Sprintf("%d.%d.%d.0/24", v4[0], v4[1], v4[2])
				if !slices.Contains(nets, n) {
					nets = append(nets, n)
				}
			}
		}
	}
	return nets
}
