// Package portscan: какие порты адреса отвечают и кто на них - по ответу самого сервиса. Голому соединению не верим:
// VPN и прозрачный прокси принимают любое. Подбора паролей и входа нет, только приветствие.
package portscan

import (
	"bytes"
	"crypto/tls"
	"net"
	"slices"
	"strconv"
	"time"

	"github.com/TimurTurdyev/tim/internal/dnsq"
	"github.com/TimurTurdyev/tim/internal/par"
	"github.com/TimurTurdyev/tim/internal/ui"
	"github.com/TimurTurdyev/tim/internal/webnet"
)

const (
	dialTimeout  = 2500 * time.Millisecond
	greetTimeout = 3 * time.Second
	retryWithin  = 1500 * time.Millisecond // ответили быстро, но не так: пробуем по-другому; молчунов второй раз не ждём
	width        = 64
	replyLimit   = 8 << 10
)

var (
	// controlPorts: на настоящем сервере закрыты. Приняли оба - соединения принимает кто-то на пути.
	controlPorts = []int{1, 4}

	risks = map[string]string{
		"mysql":         "пароль базы перебирают из интернета круглые сутки",
		"postgresql":    "пароль базы перебирают из интернета круглые сутки",
		"mssql":         "пароль базы перебирают из интернета круглые сутки",
		"redis":         "часто без пароля: ключи читают и стирают, а через настройки пишут файлы на сервер",
		"mongodb":       "без авторизации базу выкачивают и оставляют требование выкупа",
		"elasticsearch": "без пароля отдаёт все индексы любому",
		"memcached":     "отдаёт и меняет кэш любому",
		"couchdb":       "открытый API читают и меняют извне",
		"influxdb":      "без пароля отдаёт метрики и пускает писать",
		"docker":        "это root на сервере: любой запустит контейнер с диском хоста",
		"telnet":        "пароль идёт открытым текстом",
		"vnc":           "рабочий стол перебирают паролями, шифрования часто нет",
		"rdp":           "рабочий стол перебирают паролями и бьют по известным уязвимостям",
		"rsync":         "модули без пароля отдают файлы любому",
	}
)

// Service: ответивший порт. Silent - соединение принято, но сервис не назвался.
type Service struct {
	Port    int    `json:"port"`
	Name    string `json:"name"`
	Product string `json:"product,omitempty"`
	Cert    string `json:"cert,omitempty"` // имя в сертификате
	Silent  bool   `json:"silent,omitempty"`
}

// Risk: почему этому сервису не место в интернете; пусто - место обычное.
func (s Service) Risk() string { return risks[s.Name] }

// String: "22 ssh OpenSSH 9.6p1".
func (s Service) String() string {
	text := strconv.Itoa(s.Port) + " " + s.Name
	if s.Product != "" {
		text += " " + s.Product
	}
	return text
}

// Result: что ответило. Intercepted - сеть на пути принимает любое соединение, молчащие порты не в счёт; Faked - она ещё
// и отвечает сама, проверять нечего.
type Result struct {
	Services    []Service `json:"services,omitempty"`
	Intercepted bool      `json:"intercepted,omitempty"`
	Faked       bool      `json:"faked,omitempty"`
}

// Covers: этот порт мы проверяем сами.
func Covers(port int) bool {
	return slices.ContainsFunc(probes, func(p probe) bool { return p.port == port })
}

// Scan проверяет порты адреса; host - имя для HTTP и TLS, пусто - сам адрес.
func Scan(ip, host string) Result {
	if host == "" {
		host = ip
	}
	var r Result
	type control struct{ open, spoke bool }
	checks := par.Map(controlPorts, len(controlPorts), func(port int) control {
		_, reply, open := talk(probe{port: port}, ip, host)
		return control{open, len(reply) > 0}
	})
	r.Intercepted = !slices.ContainsFunc(checks, func(c control) bool { return !c.open })
	if r.Faked = slices.ContainsFunc(checks, func(c control) bool { return c.spoke }); r.Faked {
		return r
	}
	for _, s := range par.Map(probes, width, func(p probe) *Service {
		if p.port == 53 && dnsq.Hijacked() { // за любой адрес ответит тот, кто перехватил порт
			return nil
		}
		return look(p, ip, host, r.Intercepted)
	}) {
		if s != nil {
			r.Services = append(r.Services, *s)
		}
	}
	return r
}

// look: один порт; nil - закрыт или неотличим от закрытого.
func look(p probe, ip, host string, intercepted bool) *Service {
	start := time.Now()
	cert, reply, open := talk(p, ip, host)
	if !open {
		return nil
	}
	if s := named(p, cert, reply); s != nil {
		return s
	}
	// HTTP на порту оказался за TLS, или наоборот.
	if p.ask == nil && !p.greets && time.Since(start) < retryWithin {
		other := p
		other.tls = !p.tls
		if other.name = "http"; other.tls {
			other.name = "https"
		}
		cert, reply, _ = talk(other, ip, host)
		if s := named(other, cert, reply); s != nil {
			return s
		}
	}
	if intercepted {
		return nil
	}
	return &Service{Port: p.port, Name: p.name, Silent: true}
}

func named(p probe, cert string, reply []byte) *Service {
	name, product, ok := classify(p, reply)
	if !ok {
		return nil
	}
	return &Service{Port: p.port, Name: name, Product: ui.Plain(product), Cert: ui.Plain(cert)}
}

// talk: соединиться, поздороваться по правилам порта и прочитать начало ответа.
func talk(p probe, ip, host string) (cert string, reply []byte, open bool) {
	conn, err := webnet.Dial("tcp", net.JoinHostPort(ip, strconv.Itoa(p.port)), dialTimeout)
	if err != nil {
		return "", nil, false
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(greetTimeout))
	if p.tls {
		c := tls.Client(conn, &tls.Config{ServerName: host, InsecureSkipVerify: true, MinVersion: tls.VersionTLS10}) //nolint:gosec // смотрим, кто ответит
		if c.Handshake() != nil {
			return "", nil, true
		}
		if certs := c.ConnectionState().PeerCertificates; len(certs) > 0 {
			cert = certs[0].Subject.CommonName
			if cert == "" && len(certs[0].DNSNames) > 0 {
				cert = certs[0].DNSNames[0]
			}
		}
		conn = c
	}
	switch {
	case p.greets:
	case p.ask != nil:
		_, _ = conn.Write(p.ask)
	default:
		path := p.path
		if path == "" {
			path = "/"
		}
		_, _ = conn.Write([]byte("GET " + path + " HTTP/1.1\r\nHost: " + host + "\r\nUser-Agent: tim\r\nAccept: */*\r\nConnection: close\r\n\r\n"))
	}
	return cert, read(conn, p), true
}

// read: ответ до узнавания или до предела; приветствие короткое, страница HTTP приходит кусками.
func read(conn net.Conn, p probe) []byte {
	var reply []byte
	chunk := make([]byte, 2048)
	for len(reply) < replyLimit {
		n, err := conn.Read(chunk)
		reply = append(reply, chunk[:n]...)
		if err != nil {
			break
		}
		if _, _, ok := classify(p, reply); ok && (p.greets || p.ask != nil) && whole(reply) {
			break
		}
	}
	return reply
}

// whole: строка приветствия дочитана; двоичный ответ приходит одним куском.
func whole(reply []byte) bool {
	return bytes.IndexByte(reply, '\n') >= 0 || len(reply) > 0 && (reply[0] < ' ' || reply[0] > '~') || len(reply) == 1
}
