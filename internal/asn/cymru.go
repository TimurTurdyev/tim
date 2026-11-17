package asn

import (
	"io"
	"net"
	"regexp"
	"slices"
	"strings"
	"time"
)

// Сети CDN и крупных облаков: соседние домены на их адресах чужие, а сами сети слишком широкие для обхода.
var cdnWords = []string{"cloudflare", "akamai", "fastly", "cloudfront", "amazon", "google", "microsoft", "azure",
	"ddos-guard", "stormwall", "qrator", "incapsula", "imperva", "sucuri", "stackpath", "bunny", "cdnvideo", "ngenix", "gcore", "selectel-cdn"}

// edgeWords: сети, где адрес - узел раздачи, а не сервер сайта; hostingWords - хостинг той же компании.
// edgeNames: CDN внутри облака видна только по имени - обратному или в CNAME.
var (
	edgeWords = []string{"cloudflare", "akamai", "fastly", "cloudfront", "ddos-guard", "stormwall", "qrator", "incapsula", "imperva",
		"sucuri", "stackpath", "bunny", "cdnvideo", "ngenix", "gcore", "selectel-cdn", "edgio", "limelight", "cachefly"}
	hostingWords = []string{"linode", "connected cloud"}
	edgeNames    = []string{"cloudfront.net", "cdn.cloudflare.net", "akamaiedge.net", "akamai.net", "akamaized.net", "akamaitechnologies.com",
		"edgekey.net", "edgesuite.net", "fastly.net", "fastlylb.net", "azureedge.net", "azurefd.net", "b-cdn.net", "cdn77.org",
		"kxcdn.com", "edgecastcdn.net", "llnwd.net", "incapdns.net", "impervadns.net", "gcdn.co", "cdngc.net", "cdnvideo.ru", "ngenix.net",
		"trbcdn.net", "yccdn.cloud.yandex.net"}
)

var regionTail = regexp.MustCompile(`, [A-Z]{2}$`)

// ASInfo: к какой сети принадлежит адрес.
type ASInfo struct {
	ASN     string `json:"asn,omitempty"`
	Prefix  string `json:"prefix,omitempty"`
	Country string `json:"country,omitempty"`
	Name    string `json:"name,omitempty"`
}

// Owner: владелец сети словами. В реестрах имя вида "AMAZON-02 - Amazon.com, Inc., US": короткое имя и страна лишние.
func (i ASInfo) Owner() string {
	name := regionTail.ReplaceAllString(strings.TrimSpace(i.Name), "")
	if _, org, ok := strings.Cut(name, " - "); ok && strings.TrimSpace(org) != "" {
		name = org
	}
	return strings.Join(strings.Fields(strings.ReplaceAll(name, "_", " ")), " ") // вместо кавычек в реестре подчёркивания
}

// Cymru спрашивает сведения обо всех адресах одним соединением.
func Cymru(ips []string) map[string]ASInfo {
	if len(ips) == 0 {
		return nil
	}
	conn, err := net.DialTimeout("tcp", "whois.cymru.com:43", 5*time.Second)
	if err != nil {
		return nil
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(15 * time.Second))
	q := "begin\nverbose\n" + strings.Join(ips, "\n") + "\nend\n"
	if _, err := io.WriteString(conn, q); err != nil {
		return nil
	}
	data, _ := io.ReadAll(io.LimitReader(conn, 1<<20))
	return ParseCymru(string(data))
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

// IsEdge: адрес - узел CDN или защиты от DDoS: настоящий сервер за ним не виден. Хостинг и облака сюда не
// входят, там адрес обычно свой. ptr - обратное имя адреса, cname - куда ведут имена, которые на него смотрят.
func IsEdge(asName, ptr string, cname ...string) bool {
	for _, n := range append(cname, ptr) {
		n = strings.TrimSuffix(strings.ToLower(n), ".")
		if slices.ContainsFunc(edgeNames, func(e string) bool { return n == e || strings.HasSuffix(n, "."+e) }) {
			return true
		}
	}
	has := func(words []string) bool {
		return slices.ContainsFunc(words, func(w string) bool { return strings.Contains(strings.ToLower(asName), w) })
	}
	return has(edgeWords) && !has(hostingWords)
}
