// Package internetdb: что сканеры Shodan уже видели на адресе - порты, продукты, уязвимости по номеру версии, имена.
// Бесплатно и без ключа; к самому адресу запросов нет. Данным до недели.
package internetdb

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/TimurTurdyev/tim/internal/cache"
	"github.com/TimurTurdyev/tim/internal/paths"
	"github.com/TimurTurdyev/tim/internal/ui"
)

const ttl = 24 * time.Hour

var client = &http.Client{Timeout: 6 * time.Second}

// products: как зовут продукт те, кто им пользуется; остальные - своим именем из CPE.
var products = map[string]string{
	"http_server": "Apache", "openssh": "OpenSSH", "ubuntu_linux": "Ubuntu", "debian_linux": "Debian", "centos": "CentOS",
	"jquery": "jQuery", "php": "PHP", "mysql": "MySQL", "mariadb": "MariaDB", "postgresql": "PostgreSQL", "redis": "Redis",
	"mongodb": "MongoDB", "elasticsearch": "Elasticsearch", "internet_information_services": "IIS", "exim": "Exim",
	"postfix": "Postfix", "dovecot": "Dovecot", "proftpd": "ProFTPD", "pure-ftpd": "Pure-FTPd", "bind": "BIND",
	"node.js": "Node.js", "wordpress": "WordPress", "bootstrap": "Bootstrap", "bitrix24": "Bitrix24", "linux_kernel": "Linux",
	"windows": "Windows", "openssl": "OpenSSL", "gunicorn": "Gunicorn", "tomcat": "Tomcat", "openresty": "OpenResty",
	"python": "Python", "mailman": "Mailman", "cpanel": "cPanel", "plesk": "Plesk", "angie": "Angie",
}

// Info: что видно на адресе. CVE - догадка по номеру версии: дистрибутивы чинят, не меняя номер.
type Info struct {
	Ports    []int    `json:"ports,omitempty"`
	Products []string `json:"products,omitempty"`
	CVE      []string `json:"cve,omitempty"`
	Tags     []string `json:"tags,omitempty"`
	Names    []string `json:"names,omitempty"`
}

// Empty: на адресе ничего не видели.
func (i Info) Empty() bool { return len(i.Ports) == 0 && len(i.Products) == 0 }

// CDN: сканер узнал узел раздачи.
func (i Info) CDN() bool { return slices.Contains(i.Tags, "cdn") }

// Look: что видно на адресе; пустой ответ - данных нет. Кэш сутки.
func Look(ip string) (Info, error) {
	return cache.Get(cache.Path(paths.State("internetdb"), ip), ttl, func() (Info, error) {
		resp, err := client.Get("https://internetdb.shodan.io/" + url.PathEscape(ip))
		if err != nil {
			return Info{}, err
		}
		defer func() { _ = resp.Body.Close() }()
		switch resp.StatusCode {
		case http.StatusOK:
		case http.StatusNotFound: // про адрес ничего не знают
			return Info{}, nil
		default:
			return Info{}, errors.New("InternetDB ответил " + ui.Plain(resp.Status))
		}
		data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		if err != nil {
			return Info{}, err
		}
		return Parse(data)
	})
}

// Parse разбирает ответ InternetDB; CPE превращаются в названия продуктов.
func Parse(data []byte) (Info, error) {
	var raw struct {
		Ports     []int    `json:"ports"`
		CPEs      []string `json:"cpes"`
		Vulns     []string `json:"vulns"`
		Tags      []string `json:"tags"`
		Hostnames []string `json:"hostnames"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return Info{}, err
	}
	info := Info{Ports: raw.Ports, CVE: plain(raw.Vulns), Tags: plain(raw.Tags), Names: plain(raw.Hostnames)}
	for _, c := range raw.CPEs {
		if p := Product(c); p != "" && !slices.Contains(info.Products, p) {
			info.Products = append(info.Products, p)
		}
	}
	slices.Sort(info.Ports)
	slices.Sort(info.CVE)
	return info, nil
}

// Product: название и версия из CPE - "cpe:/a:openbsd:openssh:6.6.1p1" -> "OpenSSH 6.6.1p1".
func Product(cpe string) string {
	body, ok := strings.CutPrefix(cpe, "cpe:/")
	if !ok {
		if body, ok = strings.CutPrefix(cpe, "cpe:2.3:"); !ok {
			return ""
		}
	}
	f := strings.Split(body, ":")
	if len(f) < 3 || f[2] == "" {
		return ""
	}
	name := strings.ToLower(unescape(f[2]))
	if known := products[name]; known != "" {
		name = known
	} else {
		name = strings.ReplaceAll(name, "_", " ")
	}
	if len(f) > 3 && f[3] != "" && f[3] != "*" && f[3] != "-" {
		name += " " + unescape(f[3])
	}
	return ui.Plain(name)
}

func unescape(s string) string {
	if u, err := url.PathUnescape(s); err == nil {
		s = u
	}
	return strings.ReplaceAll(s, `\`, "")
}

func plain(list []string) []string {
	out := make([]string, 0, len(list))
	for _, s := range list {
		out = append(out, ui.Plain(s))
	}
	return out
}
