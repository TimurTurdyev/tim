package portscan

import (
	"regexp"
	"slices"
	"strings"
)

// Как порт назовёт себя: greets - сервис здоровается первым, asks - ответит на наши слова, tls - сначала TLS.
// Список - то, что чаще всего смотрит наружу; остальные порты не трогаем.
var probes = []probe{
	{port: 21, name: "ftp", greets: true},
	{port: 22, name: "ssh", greets: true},
	{port: 23, name: "telnet", greets: true},
	{port: 25, name: "smtp", greets: true},
	{port: 53, name: "dns", ask: dnsAsk},
	{port: 80, name: "http"},
	{port: 110, name: "pop3", greets: true},
	{port: 143, name: "imap", greets: true},
	{port: 443, name: "https", tls: true},
	{port: 465, name: "smtps", tls: true, greets: true},
	{port: 587, name: "smtp", greets: true},
	{port: 873, name: "rsync", greets: true},
	{port: 993, name: "imaps", tls: true, greets: true},
	{port: 995, name: "pop3s", tls: true, greets: true},
	{port: 1433, name: "mssql", ask: mssqlAsk},
	{port: 2082, name: "http"},
	{port: 2083, name: "https", tls: true},
	{port: 2086, name: "http"},
	{port: 2087, name: "https", tls: true},
	{port: 2222, name: "ssh", greets: true},
	{port: 2375, name: "http", path: "/version"},
	{port: 2376, name: "https", tls: true, path: "/version"},
	{port: 3000, name: "http"},
	{port: 3128, name: "http"},
	{port: 3306, name: "mysql", greets: true},
	{port: 3389, name: "rdp", ask: rdpAsk},
	{port: 4443, name: "https", tls: true},
	{port: 5000, name: "http"},
	{port: 5432, name: "postgresql", ask: postgresAsk},
	{port: 5601, name: "http"},
	{port: 5672, name: "amqp", ask: []byte("AMQP\x00\x00\x09\x01")},
	{port: 5900, name: "vnc", greets: true},
	{port: 5984, name: "http"},
	{port: 6379, name: "redis", ask: []byte("PING\r\n")},
	{port: 6443, name: "https", tls: true, path: "/version"},
	{port: 8000, name: "http"},
	{port: 8006, name: "https", tls: true},
	{port: 8080, name: "http"},
	{port: 8081, name: "http"},
	{port: 8086, name: "http", path: "/ping"},
	{port: 8088, name: "http"},
	{port: 8443, name: "https", tls: true},
	{port: 8888, name: "http"},
	{port: 9000, name: "http"},
	{port: 9090, name: "http"},
	{port: 9091, name: "http"},
	{port: 9100, name: "http"},
	{port: 9200, name: "http"},
	{port: 9443, name: "https", tls: true},
	{port: 10000, name: "http"},
	{port: 11211, name: "memcached", ask: []byte("version\r\n")},
	{port: 15672, name: "http"},
	{port: 27017, name: "http"},
}

var (
	dnsAsk = []byte{0, 17, 0x74, 0x6d, 1, 0, 0, 1, 0, 0, 0, 0, 0, 0, 0, 0, 2, 0, 1} // NS корня по TCP
	// mssqlAsk: PRELOGIN - сервер ответит своим пакетом, вход не начинается.
	mssqlAsk    = []byte{0x12, 1, 0, 26, 0, 0, 1, 0, 0, 0, 11, 0, 6, 1, 0, 17, 0, 1, 0xff, 0, 0, 0, 0, 0, 0, 2}
	rdpAsk      = []byte{3, 0, 0, 19, 14, 0xe0, 0, 0, 0, 0, 0, 1, 0, 8, 0, 3, 0, 0, 0}
	postgresAsk = []byte{0, 0, 0, 8, 0x04, 0xd2, 0x16, 0x2f} // SSLRequest

	mailProduct = regexp.MustCompile(`(?i)\b(postfix|exim [\d.]+|exim|sendmail|dovecot|proftpd [\d.]+\w*|proftpd|pure-ftpd|vsftpd [\d.]+|filezilla server [\d.]+|` +
		`microsoft esmtp|haraka|courier|cyrus|zimbra|mdaemon|opensmtpd)\b`)
	serverHeader = regexp.MustCompile(`(?im)^server:[ \t]*([^\r\n]{1,80})`)
)

// webSigns: кто отвечает по HTTP - по признакам в ответе; порядок важен.
var webSigns = []struct{ sign, name string }{
	{`"cluster_name"`, "elasticsearch"}, {"you know, for search", "elasticsearch"}, {"x-elastic-product", "elasticsearch"},
	{"over http on the native driver port", "mongodb"}, {"mongodb", "mongodb"},
	{`"couchdb"`, "couchdb"}, {"x-influxdb-version", "influxdb"}, {`"apiversion"`, "docker"}, {"api-version: 1.", "docker"},
	{`"gitversion"`, "kubernetes"}, {"kbn-name", "kibana"}, {"rabbitmq management", "rabbitmq"}, {"node exporter", "node_exporter"},
	{"prometheus time series", "prometheus"}, {"grafana_session", "grafana"}, {"pve-api-daemon", "proxmox"}, {"miniserv", "webmin"},
	{"<title>portainer", "portainer"}, {"cockpit", "cockpit"}, {"phpmyadmin", "phpmyadmin"},
}

// probe: порт и как с ним заговорить; без ask и greets - запрос HTTP.
type probe struct {
	port   int
	name   string
	greets bool
	tls    bool
	path   string
	ask    []byte
}

// classify: кто ответил и чем; ok = false - ответ не похож на этот сервис.
func classify(p probe, reply []byte) (name, product string, ok bool) {
	text := string(reply)
	low := strings.ToLower(text)
	switch {
	case len(reply) == 0:
		return "", "", false
	case strings.HasPrefix(text, "SSH-"):
		return "ssh", sshProduct(text), true
	case strings.HasPrefix(text, "HTTP/"):
		return web(p, text, low)
	case strings.HasPrefix(text, "RFB "):
		return "vnc", "", true
	case strings.HasPrefix(text, "@RSYNCD:"):
		return "rsync", "", true
	case strings.HasPrefix(text, "+OK") && p.name != "redis":
		return p.name, mailProduct.FindString(firstLine(text)), true
	case strings.HasPrefix(text, "* OK"):
		return p.name, mailProduct.FindString(firstLine(text)), true
	case strings.HasPrefix(text, "220"):
		name := p.name
		if p.port == 21 || strings.Contains(strings.ToLower(firstLine(text)), "ftp") {
			name = "ftp"
		}
		return name, mailProduct.FindString(firstLine(text)), true
	case strings.HasPrefix(text, "554") || strings.HasPrefix(text, "421"):
		return p.name, "", true // почта и ftp отказывают нам, но они есть
	}
	switch p.name {
	case "mysql":
		return mysql(reply)
	case "memcached":
		if v, found := strings.CutPrefix(firstLine(text), "VERSION "); found {
			return p.name, v, true
		}
		return "", "", false
	}
	if !answered(p.name, reply, low) {
		return "", "", false
	}
	return p.name, "", true
}

// answered: ответ на слова, которые понимает только этот сервис.
func answered(name string, reply []byte, low string) bool {
	switch name {
	case "telnet":
		return reply[0] == 0xff
	case "dns":
		return len(reply) > 4 && reply[2] == dnsAsk[2] && reply[3] == dnsAsk[3]
	case "postgresql":
		return len(reply) == 1 && (reply[0] == 'S' || reply[0] == 'N') || reply[0] == 'E'
	case "mssql":
		return reply[0] == 4
	case "rdp":
		return len(reply) > 5 && reply[0] == 3 && reply[5] == 0xd0
	case "amqp":
		return reply[0] == 1 || strings.HasPrefix(low, "amqp")
	case "redis":
		return slices.ContainsFunc([]string{"+pong", "-noauth", "-denied", "-err"}, func(s string) bool { return strings.HasPrefix(low, s) })
	}
	return false
}

func web(p probe, text, low string) (string, string, bool) {
	name, product := p.name, ""
	if m := serverHeader.FindStringSubmatch(text); m != nil {
		product = strings.TrimSpace(m[1])
	}
	for _, s := range webSigns {
		if strings.Contains(low, s.sign) {
			return s.name, product, true
		}
	}
	if name != "http" && name != "https" {
		name = "http"
	}
	return name, product, true
}

// sshProduct: "SSH-2.0-OpenSSH_9.6p1 Ubuntu-3ubuntu13.5" -> "OpenSSH 9.6p1 Ubuntu".
func sshProduct(banner string) string {
	parts := strings.SplitN(firstLine(banner), "-", 3)
	if len(parts) < 3 {
		return ""
	}
	words := strings.Fields(strings.ReplaceAll(parts[2], "_", " "))
	for i, w := range words {
		if i > 1 {
			words[i], _, _ = strings.Cut(w, "-")
		}
	}
	return strings.Join(words[:min(len(words), 3)], " ")
}

// mysql: пакет - 3 байта длины, номер, 0x0a и версия; 0xff - нас не пускают, но сервис это он.
func mysql(reply []byte) (string, string, bool) {
	if len(reply) < 6 {
		return "", "", false
	}
	switch reply[4] {
	case 0xff:
		return "mysql", "", true
	case 0x0a:
		v, _, _ := strings.Cut(string(reply[5:]), "\x00")
		if strings.Contains(strings.ToLower(v), "mariadb") {
			return "mysql", "MariaDB " + strings.TrimPrefix(strings.SplitN(strings.TrimPrefix(v, "5.5.5-"), "-", 2)[0], "v"), true
		}
		return "mysql", "MySQL " + strings.SplitN(v, "-", 2)[0], true
	}
	return "", "", false
}

func firstLine(s string) string {
	l, _, _ := strings.Cut(s, "\n")
	return strings.TrimRight(l, "\r")
}
