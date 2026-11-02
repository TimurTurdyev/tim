package dnsq

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/http"
	"sync"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

// dohServers: DNS поверх HTTPS - его не перехватывает VPN, который отвечает за любой адрес на порту 53.
var dohServers = []string{"https://cloudflare-dns.com/dns-query", "https://dns.google/dns-query"}

var dohResolver = &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
	return &dohConn{ctx: ctx}, nil
}}

// probe: 192.0.2.1 - адрес для документации, DNS-сервера там нет и быть не может.
const probe = "192.0.2.1:53"

// Hijacked: порт 53 перехвачен - на запрос к адресу, где сервера нет, кто-то ответил. Тогда и 1.1.1.1:53,
// и системный резолвер отвечают из чужого кэша: свежая запись может выглядеть отсутствующей до часа.
var Hijacked = sync.OnceValue(func() bool {
	msg, err := build(".", dnsmessage.TypeNS)
	if err != nil {
		return false
	}
	_, err = overUDP(probe, msg, 500*time.Millisecond)
	return err == nil
})

// Resolver: системный резолвер, а при перехвате порта 53 - через DoH. Для проверок чужих доменов (почта,
// записи, двойники), где важен ответ самой зоны, а не кэша на пути.
func Resolver() *net.Resolver {
	if !Hijacked() {
		return net.DefaultResolver
	}
	return dohResolver
}

func overHTTPS(ctx context.Context, msg []byte) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var last error
	for _, u := range dohServers {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(msg))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/dns-message")
		req.Header.Set("Accept", "application/dns-message")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			last = err
			continue
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		_ = resp.Body.Close()
		if err == nil && resp.StatusCode != http.StatusOK {
			err = errors.New("DoH " + u + ": " + resp.Status)
		}
		if err != nil {
			last = err
			continue
		}
		return body, nil
	}
	return nil, last
}

// dohConn: соединение для резолвера Go, которое отправляет каждый запрос через DoH. Соединение не
// PacketConn, поэтому резолвер говорит с ним как по tcp: два байта длины, потом сообщение.
type dohConn struct {
	ctx context.Context
	out bytes.Buffer
}

func (c *dohConn) Write(b []byte) (int, error) {
	if len(b) < 2 {
		return 0, errors.New("dnsq: короткий запрос")
	}
	resp, err := overHTTPS(c.ctx, b[2:])
	if err != nil {
		return 0, err
	}
	c.out.Reset()
	_ = binary.Write(&c.out, binary.BigEndian, uint16(len(resp)))
	c.out.Write(resp)
	return len(b), nil
}

func (c *dohConn) Read(b []byte) (int, error) { return c.out.Read(b) }

func (c *dohConn) Close() error                     { return nil }
func (c *dohConn) LocalAddr() net.Addr              { return dohAddr{} }
func (c *dohConn) RemoteAddr() net.Addr             { return dohAddr{} }
func (c *dohConn) SetDeadline(time.Time) error      { return nil }
func (c *dohConn) SetReadDeadline(time.Time) error  { return nil }
func (c *dohConn) SetWriteDeadline(time.Time) error { return nil }

type dohAddr struct{}

func (dohAddr) Network() string { return "doh" }
func (dohAddr) String() string  { return "doh" }
