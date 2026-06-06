package inspect

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strings"
)

var generatorRe = regexp.MustCompile(`(?i)<meta[^>]+name=["']generator["'][^>]+content=["']([^"']+)`)

type Page struct {
	Final     string   `json:"final"`
	Status    int      `json:"status"`
	Redirects []string `json:"redirects,omitempty"`
	Server    string   `json:"server,omitempty"`
	PoweredBy string   `json:"powered_by,omitempty"`
	CMS       string   `json:"cms,omitempty"`
}

// HTTP идёт по редиректам и читает первые 256 КБ тела для распознавания CMS.
func HTTP(url string) (*Page, error) {
	var redirects []string
	client := &http.Client{Timeout: Timeout, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return errors.New("больше 10 редиректов")
		}
		redirects = append(redirects, req.URL.String())
		return nil
	}}
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh) tim")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 256<<10))
	var cookies []string
	for _, c := range resp.Cookies() {
		cookies = append(cookies, c.Name)
	}
	return &Page{
		Final:     resp.Request.URL.String(),
		Status:    resp.StatusCode,
		Redirects: redirects,
		Server:    resp.Header.Get("Server"),
		PoweredBy: resp.Header.Get("X-Powered-By"),
		CMS:       DetectCMS(resp.Header, cookies, string(body)),
	}, nil
}

// DetectCMS угадывает движок по meta generator, cookie, заголовкам и характерным путям в HTML.
func DetectCMS(h http.Header, cookies []string, html string) string {
	if m := generatorRe.FindStringSubmatch(html); m != nil {
		return strings.TrimSpace(m[1])
	}
	has := func(name string) bool {
		for _, c := range cookies {
			if strings.EqualFold(c, name) || strings.HasPrefix(strings.ToLower(c), strings.ToLower(name)) {
				return true
			}
		}
		return false
	}
	low := strings.ToLower(html)
	powered := strings.ToLower(h.Get("X-Powered-By"))
	switch {
	case strings.Contains(low, "/wp-content/") || strings.Contains(low, "/wp-includes/"):
		return "WordPress"
	case has("OCSESSID") || strings.Contains(low, "catalog/view/theme") || strings.Contains(low, "index.php?route="):
		return "OpenCart"
	case strings.Contains(low, "/bitrix/") || has("BITRIX_SM"):
		return "1C-Bitrix"
	case strings.Contains(low, "tilda.ws") || strings.Contains(low, "tildacdn"):
		return "Tilda"
	case has("laravel_session") || has("XSRF-TOKEN") && strings.Contains(low, "/build/assets/"):
		return "Laravel"
	case strings.Contains(low, "/_nuxt/"):
		return "Nuxt"
	case strings.Contains(low, "/_next/") || strings.Contains(powered, "next.js"):
		return "Next.js"
	case strings.Contains(low, "/assets/chunks/") && strings.Contains(low, "vitepress"):
		return "VitePress"
	case strings.Contains(low, "/media/jui/") || strings.Contains(low, "joomla"):
		return "Joomla"
	case strings.Contains(low, "drupal-settings-json") || h.Get("X-Drupal-Cache") != "":
		return "Drupal"
	case strings.Contains(low, "/assets/components/") && has("PHPSESSID"):
		return "MODX"
	}
	return ""
}

type Geo struct {
	IP      string `json:"ip"`
	Country string `json:"country,omitempty"`
	City    string `json:"city,omitempty"`
	ISP     string `json:"isp,omitempty"`
	ASN     int    `json:"asn,omitempty"`
}

// IPGeo спрашивает ipwho.is: страна, город, провайдер.
func IPGeo(ip string) (*Geo, error) {
	resp, err := httpClient.Get("https://ipwho.is/" + ip)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	var r struct {
		Success    bool   `json:"success"`
		Message    string `json:"message"`
		Country    string `json:"country"`
		City       string `json:"city"`
		Connection struct {
			ISP string `json:"isp"`
			ASN int    `json:"asn"`
		} `json:"connection"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&r); err != nil {
		return nil, err
	}
	if !r.Success {
		return nil, errors.New("ipwho.is: " + r.Message)
	}
	return &Geo{IP: ip, Country: r.Country, City: r.City, ISP: r.Connection.ISP, ASN: r.Connection.ASN}, nil
}
