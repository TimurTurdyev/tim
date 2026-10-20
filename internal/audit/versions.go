package audit

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/TimurTurdyev/tim/internal/cache"
	"github.com/TimurTurdyev/tim/internal/level"
	"github.com/TimurTurdyev/tim/internal/paths"
)

const releasesTTL = 24 * time.Hour

var (
	generatorRe = regexp.MustCompile(`(?i)<meta[^>]+name=["']generator["'][^>]+content=["']([^"']+)`)
	productRe   = regexp.MustCompile(`(?i)\b(nginx|apache|php|wordpress|drupal|joomla)!?[/ ]v?(\d+(?:\.\d+){1,2})`)
	distroRe    = regexp.MustCompile(`(?i)ubuntu|debian|deb\d|\.el\d`)
)

// eolSlugs: продукт -> имя на endoflife.date.
var eolSlugs = map[string]string{"nginx": "nginx", "apache": "apache-http-server", "php": "php",
	"wordpress": "wordpress", "drupal": "drupal", "joomla": "joomla"}

// Software: что сайт сам говорит о своих версиях.
type Software struct {
	Product string `json:"product"`
	Version string `json:"version"`
	Header  bool   `json:"header"` // из заголовка, а не из страницы
	Distro  bool   `json:"distro"` // пакет дистрибутива: заплатки ставит Ubuntu или Debian, номер не меняется
}

// Release: цикл версий с endoflife.date.
type Release struct {
	Name    string `json:"name"`
	IsEol   bool   `json:"isEol"`
	EolFrom string `json:"eolFrom"`
}

// ParseSoftware: версии из Server, X-Powered-By и meta generator.
func ParseSoftware(server, poweredBy, html string) []Software {
	var out []Software
	// Debian и Ubuntu держат номер версии, а заплатки шлют сами: если сервер из их пакетов, то и PHP
	// рядом скорее всего оттуда же.
	distro := distroRe.MatchString(server)
	scan := func(s string, header bool) {
		for _, m := range productRe.FindAllStringSubmatch(s, -1) {
			out = append(out, Software{Product: strings.ToLower(m[1]), Version: m[2], Header: header, Distro: distro || distroRe.MatchString(s)})
		}
	}
	scan(server, true)
	scan(poweredBy, true)
	if m := generatorRe.FindStringSubmatch(html); m != nil {
		scan(m[1], false)
	}
	return out
}

// ParseReleases: ответ endoflife.date /api/v1/products/<продукт>/.
func ParseReleases(data []byte) ([]Release, error) {
	var resp struct {
		Result struct {
			Releases []Release `json:"releases"`
		} `json:"result"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, err
	}
	return resp.Result.Releases, nil
}

// VersionFindings: версии в заголовках и версии без обновлений безопасности. У пакетов дистрибутива
// срок не сверяется: заплатки приходят от Ubuntu или Debian, а номер версии остаётся старым.
func VersionFindings(sw []Software, releases map[string][]Release) []Finding {
	var reported, shown []string
	var findings []Finding
	for _, s := range sw {
		if s.Header {
			shown = append(shown, s.Product+" "+s.Version)
		}
		if s.Distro {
			continue
		}
		list := releases[s.Product]
		r, ok := cycle(list, s.Version)
		if !ok || !r.IsEol || slices.Contains(reported, s.Product) {
			continue
		}
		reported = append(reported, s.Product)
		fix := "обновить"
		if len(list) > 0 {
			fix = "обновить до поддерживаемой версии, сейчас последняя ветка " + list[0].Name
		}
		findings = append(findings, Finding{Area: "версии", Level: level.Bad,
			Text: fmt.Sprintf("%s %s без обновлений безопасности с %s", s.Product, r.Name, r.EolFrom),
			Why:  "известные дыры этой версии уже никто не закрывает", Fix: fix})
	}
	if len(shown) > 0 {
		findings = append(findings, Finding{Area: "версии", Level: level.Info, Text: "сервер называет версии: " + strings.Join(shown, ", "),
			Why: "по номеру версии бот сразу берёт подходящий эксплойт", Fix: "server_tokens off; в nginx, expose_php = Off в php.ini"})
	}
	return findings
}

// cycle: ветка, к которой относится версия, - самое длинное совпадение по точкам: 1.18.0 -> 1.18.
func cycle(list []Release, version string) (Release, bool) {
	var best Release
	for _, r := range list {
		if (version == r.Name || strings.HasPrefix(version, r.Name+".")) && len(r.Name) > len(best.Name) {
			best = r
		}
	}
	return best, best.Name != ""
}

// releasesOf: ветки продуктов с endoflife.date, кэш на сутки; без сети - что есть в кэше.
func releasesOf(sw []Software) map[string][]Release {
	out := map[string][]Release{}
	for _, s := range sw {
		slug, ok := eolSlugs[s.Product]
		if !ok || s.Distro {
			continue
		}
		list, err := cache.Get(cache.Path(paths.State("eol"), slug), releasesTTL, func() ([]Release, error) {
			return fetchReleases(slug)
		})
		if err == nil {
			out[s.Product] = list
		}
	}
	return out
}

func fetchReleases(slug string) ([]Release, error) {
	client := &http.Client{Timeout: timeout}
	resp, err := client.Get("https://endoflife.date/api/v1/products/" + slug + "/")
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("endoflife.date: %s", resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	return ParseReleases(data)
}
