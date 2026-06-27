package asn

import (
	"io"
	"net"
	"strings"
	"time"
)

// Сети CDN и крупных облаков: соседние домены на их адресах чужие, а сами сети слишком широкие для обхода.
var cdnWords = []string{"cloudflare", "akamai", "fastly", "cloudfront", "amazon", "google", "microsoft", "azure",
	"ddos-guard", "stormwall", "qrator", "incapsula", "imperva", "sucuri", "stackpath", "bunny", "cdnvideo", "ngenix", "gcore", "selectel-cdn"}

// ASInfo: к какой сети принадлежит адрес.
type ASInfo struct {
	ASN     string `json:"asn,omitempty"`
	Prefix  string `json:"prefix,omitempty"`
	Country string `json:"country,omitempty"`
	Name    string `json:"name,omitempty"`
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
