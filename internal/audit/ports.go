package audit

import (
	"bytes"
	"crypto/tls"
	"fmt"
	"net"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/TimurTurdyev/tim/internal/level"
	"github.com/TimurTurdyev/tim/internal/par"
	"github.com/TimurTurdyev/tim/internal/webnet"
)

const portTimeout = 3 * time.Second

// DBPorts: что не должно отвечать из интернета. 2375 - Docker API без TLS, это root на машине.
// Знают и аудит снаружи, и отчёт о сервере по ssh.
var DBPorts = map[int]string{
	3306: "MySQL", 5432: "PostgreSQL", 6379: "Redis", 27017: "MongoDB", 9200: "Elasticsearch",
	11211: "Memcached", 5984: "CouchDB", 8086: "InfluxDB", 2375: "Docker API без TLS", 2376: "Docker API",
}

var (
	httpGet = []byte("GET / HTTP/1.0\r\n\r\n")
	// portHello: что сказать сервису, чтобы он себя назвал; MySQL здоровается первым.
	portHello = map[int][]byte{
		5432:  {0, 0, 0, 8, 0x04, 0xd2, 0x16, 0x2f}, // SSLRequest
		6379:  []byte("PING\r\n"),
		27017: httpGet,
		9200:  httpGet,
		11211: []byte("version\r\n"),
		5984:  httpGet,
		8086:  []byte("GET /ping HTTP/1.0\r\n\r\n"),
		2375:  []byte("GET /version HTTP/1.0\r\n\r\n"),
		2376:  []byte("GET /version HTTP/1.0\r\n\r\n"),
	}
	portWhy = map[int]string{
		3306:  "пароль базы перебирают из интернета круглые сутки: слабый или утёкший пароль - и база у чужих",
		5432:  "пароль базы перебирают из интернета круглые сутки: слабый или утёкший пароль - и база у чужих",
		6379:  "Redis часто без пароля: ключи читают и стирают, а через CONFIG SET пишут файлы на сервер",
		27017: "MongoDB без авторизации - частая причина утечек: базу выкачивают и оставляют требование выкупа",
		9200:  "Elasticsearch без пароля отдаёт все индексы любому",
		11211: "Memcached без пароля отдаёт и меняет кэш любому",
		5984:  "CouchDB с открытым API читают и меняют извне",
		8086:  "InfluxDB без пароля отдаёт метрики и пускает писать",
		2375:  "Docker API без TLS - это root на сервере: любой запустит контейнер с диском хоста",
		2376:  "Docker API пускает без сертификата клиента - это root на сервере",
	}
	// controlPorts: на настоящем сервере закрыты. Ответили оба - соединения принимает кто-то на пути
	// (VPN, прозрачный прокси), и открытый порт ничего не значит.
	controlPorts = []int{1, 4}
)

// portScan: подтверждённые сервисы на портах баз; intercepted - проверить нельзя, сеть принимает всё.
type portScan struct {
	open        []int
	intercepted bool
}

// PortFindings: порты баз, которые отвечают из интернета, - каждая находка по ответу самого сервиса.
func PortFindings(scan portScan) []Finding {
	if scan.intercepted {
		return []Finding{{Area: "порты", Level: level.Info, Text: "порты баз не проверены: сеть на пути принимает любое соединение",
			Why: "так ведут себя VPN и прозрачный прокси: открытый порт ничего не говорит о сервере",
			Fix: "проверить без VPN или с сервера в другой сети", Intrusive: true}}
	}
	var out []Finding
	for _, p := range scan.open {
		fix := fmt.Sprintf("слушать на 127.0.0.1, снаружи ходить через ssh-туннель: tim tunnel port %d <сервер>", p)
		if p == 2375 || p == 2376 {
			fix = "закрыть порт в файрволе, Docker на сервере - через ssh: DOCKER_HOST=ssh://сервер"
		}
		out = append(out, Finding{Area: "порты", Level: level.Bad, Text: fmt.Sprintf("наружу отвечает %s (%d)", DBPorts[p], p),
			Why: portWhy[p], Fix: fix, Intrusive: true})
	}
	return out
}

// Identify: ответ на порту - это сам сервис, а не что-то, что приняло соединение. На пустой ответ и на
// чужой (страница nginx на порту базы) - нет.
func Identify(port int, reply []byte) bool {
	text := strings.ToLower(string(reply))
	switch port {
	case 3306:
		// Пакет: 3 байта длины, номер, затем 0x0a - приветствие протокола 10, 0xff - отказ "хост не пущен".
		return len(reply) > 4 && (reply[4] == 0x0a || reply[4] == 0xff)
	case 5432:
		return len(reply) > 0 && (reply[0] == 'S' || reply[0] == 'N' || reply[0] == 'E')
	case 6379:
		return slices.ContainsFunc([]string{"+pong", "-noauth", "-denied", "-err"}, func(p string) bool { return strings.HasPrefix(text, p) })
	case 27017:
		return strings.Contains(text, "mongodb")
	case 9200:
		return strings.Contains(text, "x-elastic-product") || strings.Contains(text, `"cluster_name"`) ||
			strings.Contains(text, "you know, for search")
	case 11211:
		return bytes.HasPrefix(reply, []byte("VERSION "))
	case 5984:
		return strings.Contains(text, `"couchdb"`)
	case 8086:
		return strings.Contains(text, "x-influxdb-version")
	case 2375, 2376:
		return strings.Contains(text, `"apiversion"`)
	}
	return false
}

// scanPorts: сначала контрольные порты, потом сервисы на портах баз - параллельно.
func scanPorts(host string) portScan {
	ip := hostIP(host)
	if ip == "" {
		return portScan{}
	}
	control := par.Map(controlPorts, len(controlPorts), func(p int) bool { return connects(ip, p) })
	if !slices.Contains(control, false) {
		return portScan{intercepted: true}
	}
	ports := make([]int, 0, len(DBPorts))
	for p := range DBPorts {
		ports = append(ports, p)
	}
	slices.Sort(ports)
	alive := par.Map(ports, len(ports), func(p int) bool { return Identify(p, ask(ip, p)) })
	var scan portScan
	for i, ok := range alive {
		if ok {
			scan.open = append(scan.open, ports[i])
		}
	}
	return scan
}

// hostIP: первый IPv4 сайта, иначе первый адрес.
func hostIP(host string) string {
	ips, err := net.LookupIP(host)
	if err != nil || len(ips) == 0 {
		return ""
	}
	for _, x := range ips {
		if x.To4() != nil {
			return x.String()
		}
	}
	return ips[0].String()
}

func connects(ip string, port int) bool {
	c, err := webnet.Dial("tcp", net.JoinHostPort(ip, strconv.Itoa(port)), portTimeout)
	if err != nil {
		return false
	}
	_ = c.Close()
	return true
}

// ask: соединиться, поздороваться и прочитать начало ответа; 2376 - через TLS без проверки сертификата.
func ask(ip string, port int) []byte {
	addr := net.JoinHostPort(ip, strconv.Itoa(port))
	c, err := webnet.Dial("tcp", addr, portTimeout)
	if err != nil {
		return nil
	}
	if port == 2376 {
		c = tls.Client(c, &tls.Config{InsecureSkipVerify: true}) //nolint:gosec // смотрим, пустит ли без сертификата клиента
	}
	defer func() { _ = c.Close() }()
	_ = c.SetDeadline(time.Now().Add(portTimeout))
	if hello := portHello[port]; hello != nil {
		if _, err := c.Write(hello); err != nil {
			return nil
		}
	}
	// Заголовки и тело HTTP приходят разными кусками: читаем, пока не узнали сервис или ответ не кончился.
	var reply []byte
	chunk := make([]byte, 1024)
	for len(reply) < 4096 {
		n, err := c.Read(chunk)
		reply = append(reply, chunk[:n]...)
		if err != nil || Identify(port, reply) {
			break
		}
	}
	return reply
}
