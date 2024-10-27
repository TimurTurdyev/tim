// Package domaudit: сведения о домене из публичных источников - сертификаты, адреса, живость.
package domaudit

import (
	"encoding/json"
	"errors"
	"html"
	"regexp"
	"sort"
	"strings"

	"golang.org/x/net/publicsuffix"
)

// ParseCRT разбирает ответ crt.sh: поддомены домена и другие регистрируемые домены из тех же сертификатов.
func ParseCRT(data []byte, domain string) (subs, related []string, err error) {
	var rows []struct {
		NameValue string `json:"name_value"`
		Common    string `json:"common_name"`
	}
	if err := json.Unmarshal(data, &rows); err != nil {
		return nil, nil, err
	}
	var names []string
	for _, r := range rows {
		names = append(names, strings.Split(r.NameValue, "\n")...)
		names = append(names, r.Common)
	}
	return splitNames(names, domain)
}

// ASInfo: к какой сети принадлежит адрес.
type ASInfo struct {
	ASN     string `json:"asn,omitempty"`
	Prefix  string `json:"prefix,omitempty"`
	Country string `json:"country,omitempty"`
	Name    string `json:"name,omitempty"`
}

// ParseCymru разбирает bulk-ответ whois.cymru.com: AS | IP | префикс | страна | реестр | дата | имя.
func ParseCymru(out string) map[string]ASInfo {
	m := map[string]ASInfo{}
	for _, line := range strings.Split(out, "\n") {
		f := strings.Split(line, "|")
		if len(f) < 7 {
			continue
		}
		for i := range f {
			f[i] = strings.TrimSpace(f[i])
		}
		if f[0] == "AS" || f[0] == "NA" || f[1] == "" {
			continue
		}
		m[f[1]] = ASInfo{ASN: f[0], Prefix: f[2], Country: f[3], Name: f[6]}
	}
	return m
}

var cdnWords = []string{"cloudflare", "akamai", "fastly", "cloudfront", "amazon", "google", "microsoft", "azure",
	"ddos-guard", "stormwall", "qrator", "incapsula", "imperva", "sucuri", "stackpath", "bunny", "cdnvideo", "ngenix", "gcore", "selectel-cdn"}

// IsCDN: адрес принадлежит CDN или крупному облаку. На таком адресе соседние домены чужие и ни о чём не говорят.
func IsCDN(asName string) bool {
	l := strings.ToLower(asName)
	for _, w := range cdnWords {
		if strings.Contains(l, w) {
			return true
		}
	}
	return false
}

var titleRe = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)

// Title достаёт заголовок страницы, схлопывая пробелы.
func Title(body string) string {
	m := titleRe.FindStringSubmatch(body)
	if m == nil {
		return ""
	}
	t := html.UnescapeString(m[1])
	t = strings.Join(strings.Fields(t), " ")
	if len([]rune(t)) > 70 {
		t = string([]rune(t)[:70]) + "..."
	}
	return t
}

// Tracker: счётчик или тег на странице.
type Tracker struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

var trackerRes = []struct {
	kind string
	re   *regexp.Regexp
}{
	{"Яндекс.Метрика", regexp.MustCompile(`(?:ym\(|mc\.yandex\.ru/watch/|id:\s*)(\d{6,9})`)},
	{"Google Analytics", regexp.MustCompile(`\b(G-[A-Z0-9]{6,12}|UA-\d{4,10}-\d{1,4})\b`)},
	{"Google Tag Manager", regexp.MustCompile(`\b(GTM-[A-Z0-9]{4,8})\b`)},
	{"AdSense", regexp.MustCompile(`\b(ca-pub-\d{10,20})\b`)},
	{"Facebook Pixel", regexp.MustCompile(`fbq\('init',\s*'(\d{10,20})'`)},
	{"VK Pixel", regexp.MustCompile(`\b(VK-RTRG-\d+-\w+)\b`)},
}

// Trackers: счётчики аналитики со страницы. Полезно, чтобы свериться, тот ли счётчик стоит на сайте.
func Trackers(body string) []Tracker {
	var out []Tracker
	seen := map[string]bool{}
	for _, t := range trackerRes {
		for _, m := range t.re.FindAllStringSubmatch(body, -1) {
			id := m[1]
			if seen[id] {
				continue
			}
			seen[id] = true
			out = append(out, Tracker{Kind: t.kind, ID: id})
		}
	}
	return out
}

var hostLine = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+$`)

// ParseNeighbors: список доменов на том же адресе, по строке на домен.
func ParseNeighbors(out string, self string) []string {
	var list []string
	seen := map[string]bool{self: true}
	for _, l := range strings.Split(out, "\n") {
		n := strings.ToLower(strings.TrimSpace(l))
		if !hostLine.MatchString(n) || seen[n] {
			continue
		}
		seen[n] = true
		list = append(list, n)
	}
	sort.Strings(list)
	return list
}

// Common: частые имена поддоменов для перебора через DNS.
var Common = []string{
	"www", "mail", "webmail", "smtp", "imap", "pop", "mx", "ns1", "ns2", "api", "api2", "app", "admin", "panel",
	"cp", "cpanel", "dev", "stage", "staging", "test", "demo", "beta", "old", "new", "shop", "store", "blog",
	"news", "forum", "wiki", "docs", "doc", "help", "support", "status", "cdn", "static", "assets", "img",
	"images", "media", "files", "upload", "download", "db", "mysql", "pgsql", "redis", "git", "gitlab", "jenkins",
	"ci", "build", "deploy", "vpn", "ssh", "ftp", "sftp", "backup", "monitor", "grafana", "metrics", "logs",
	"kibana", "portal", "my", "lk", "office", "crm", "erp", "lms", "edu", "m", "mobile", "ws", "socket", "chat",
}

// ParseCertSpotter разбирает ответ api.certspotter.com: имена из поля dns_names.
func ParseCertSpotter(data []byte, domain string) (subs, related []string, err error) {
	var rows []struct {
		DNSNames []string `json:"dns_names"`
	}
	if err := json.Unmarshal(data, &rows); err != nil {
		return nil, nil, err
	}
	var names []string
	for _, r := range rows {
		names = append(names, r.DNSNames...)
	}
	return splitNames(names, domain)
}

// ParseHostSearch разбирает CSV "имя,адрес" построчно.
func ParseHostSearch(out string, domain string) ([]string, error) {
	if strings.Contains(out, "API count exceeded") || strings.Contains(out, "error ") {
		return nil, errors.New(strings.TrimSpace(firstLine(out)))
	}
	var names []string
	for _, line := range strings.Split(out, "\n") {
		name, _, ok := strings.Cut(strings.TrimSpace(line), ",")
		if ok {
			names = append(names, name)
		}
	}
	subs, _, err := splitNames(names, domain)
	return subs, err
}

func firstLine(s string) string {
	l, _, _ := strings.Cut(s, "\n")
	return l
}

// splitNames делит имена на поддомены домена и другие регистрируемые домены.
func splitNames(names []string, domain string) (subs, related []string, err error) {
	domain = strings.ToLower(domain)
	seenS, seenR := map[string]bool{}, map[string]bool{}
	for _, n := range names {
		n = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(n)), "*.")
		n = strings.TrimSuffix(n, ".")
		if n == "" || strings.ContainsAny(n, " @") {
			continue
		}
		if n == domain || strings.HasSuffix(n, "."+domain) {
			if !seenS[n] {
				seenS[n] = true
				subs = append(subs, n)
			}
			continue
		}
		reg, e := publicsuffix.EffectiveTLDPlusOne(n)
		if e == nil && reg != domain && !seenR[reg] {
			seenR[reg] = true
			related = append(related, reg)
		}
	}
	sort.Strings(subs)
	sort.Strings(related)
	return subs, related, nil
}
