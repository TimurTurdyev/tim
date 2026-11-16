// Package webnet: сеть проверок сайтов - один вход для соединений и HTTP. В tim это обычная сеть;
// страница проверки сайта ставит свою, с охраной от внутренних адресов, и охрана видит каждое соединение.
package webnet

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"net/url"
	"sync"
	"syscall"
	"time"
)

// nextAddress: сколько ждать один адрес имени, прежде чем пробовать и следующий.
const nextAddress = 300 * time.Millisecond

var (
	mu      sync.Mutex
	current *state
)

// Net: как ходить в сеть.
type Net struct {
	// Control: проверка адреса перед каждым соединением, как net.Dialer.ControlContext; nil - без проверки.
	Control func(ctx context.Context, network, address string, c syscall.RawConn) error
	// Proxy: прокси для HTTP, как http.Transport.Proxy; nil - напрямую.
	Proxy func(*http.Request) (*url.URL, error)
	// Redirect: можно ли идти по редиректу, как http.Client.CheckRedirect; nil - как у Go, до 10.
	Redirect func(req *http.Request, via []*http.Request) error
}

// state: сеть и её транспорты - общие, чтобы соединения переиспользовались, а не копились.
type state struct {
	net              Net
	secure, insecure *http.Transport
}

// Use ставит сеть для всех проверок; зовётся до первой проверки.
func Use(n Net) {
	mu.Lock()
	defer mu.Unlock()
	current = build(n)
}

// Dialer: соединение с таймаутом через общий вход.
func Dialer(timeout time.Duration) *net.Dialer {
	return &net.Dialer{Timeout: timeout, ControlContext: get().net.Control}
}

// Dial: TCP или UDP с таймаутом через общий вход.
func Dial(network, address string, timeout time.Duration) (net.Conn, error) {
	return Dialer(timeout).Dial(network, address)
}

// Client: HTTP с таймаутом через общий транспорт. insecure - без проверки сертификата: он уже признан
// битым, а смотреть надо остальное.
func Client(timeout time.Duration, insecure bool) *http.Client {
	s := get()
	t := s.secure
	if insecure {
		t = s.insecure
	}
	return &http.Client{Timeout: timeout, Transport: t, CheckRedirect: s.net.Redirect}
}

// Fresh: HTTP без переиспользования соединений - для замера фаз: DNS, TCP и TLS в каждом запросе заново.
func Fresh(timeout time.Duration) *http.Client {
	n := get().net
	t := build(n).secure
	t.DisableKeepAlives = true
	return &http.Client{Timeout: timeout, Transport: t, CheckRedirect: n.Redirect}
}

// Pinned: как Fresh, но имена из known соединяются сразу со своими адресами: их уже спросили у DNS, а
// холодный системный резолвер только съест срок. insecure - без проверки сертификата.
func Pinned(timeout time.Duration, insecure bool, known map[string][]string) *http.Client {
	n := get().net
	s := build(n)
	t := s.secure
	if insecure {
		t = s.insecure
	}
	t.DisableKeepAlives = true
	dial := t.DialContext
	t.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil || len(known[host]) == 0 {
			return dial(ctx, network, address)
		}
		return dialAny(ctx, dial, network, known[host], port)
	}
	return &http.Client{Timeout: timeout, Transport: t, CheckRedirect: n.Redirect}
}

// dialAny: первое удавшееся соединение; следующий адрес пробуется, если прежний отказал или медлит.
func dialAny(ctx context.Context, dial func(context.Context, string, string) (net.Conn, error), network string, ips []string, port string) (net.Conn, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	type result struct {
		conn net.Conn
		err  error
	}
	results := make(chan result, len(ips))
	var last error
	for next, waiting := 0, 0; next < len(ips) || waiting > 0; {
		var slow <-chan time.Time
		if next < len(ips) {
			ip := ips[next]
			next++
			waiting++
			go func() {
				c, err := dial(ctx, network, net.JoinHostPort(ip, port))
				results <- result{c, err}
			}()
			slow = time.After(nextAddress)
		}
		select {
		case r := <-results:
			waiting--
			if r.err == nil {
				go func() {
					for range waiting {
						if extra := <-results; extra.conn != nil {
							_ = extra.conn.Close()
						}
					}
				}()
				return r.conn, nil
			}
			last = r.err
		case <-slow:
		}
	}
	return nil, last
}

// Follow: своё правило редиректов клиента сначала проверяется общим - его нельзя обойти.
func Follow(req *http.Request, via []*http.Request) error {
	if r := get().net.Redirect; r != nil {
		return r(req, via)
	}
	return nil
}

// NoRedirect: клиенту - ответ на сам адрес, без перехода по редиректам.
func NoRedirect(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

func get() *state {
	mu.Lock()
	defer mu.Unlock()
	if current == nil {
		current = build(Net{Proxy: http.ProxyFromEnvironment})
	}
	return current
}

func build(n Net) *state {
	transport := func(insecure bool) *http.Transport {
		t := &http.Transport{
			Proxy:               n.Proxy,
			DialContext:         (&net.Dialer{Timeout: 10 * time.Second, ControlContext: n.Control}).DialContext,
			ForceAttemptHTTP2:   true,
			MaxIdleConns:        50,
			IdleConnTimeout:     30 * time.Second,
			TLSHandshakeTimeout: 10 * time.Second,
		}
		if insecure {
			t.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // сертификат уже признан битым
		}
		return t
	}
	return &state{net: n, secure: transport(false), insecure: transport(true)}
}
