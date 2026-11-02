// Package dnsq: запросы DNS, которых нет в net: CAA и признак DNSSEC от проверяющего резолвера.
package dnsq

import (
	"context"
	"errors"
	"math/rand/v2"
	"net"
	"strings"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

// Resolvers: проверяют DNSSEC сами и ставят бит AD. Системный резолвер, особенно за VPN, может этого не делать.
var Resolvers = []string{"1.1.1.1:53", "8.8.8.8:53"}

const typeCAA dnsmessage.Type = 257

// Answer: ответ на запрос - записи и проверена ли подпись.
type Answer struct {
	Authentic bool
	Records   []dnsmessage.Resource
}

func query(server, name string, t dnsmessage.Type) (Answer, error) {
	msg, err := build(name, t)
	if err != nil {
		return Answer{}, err
	}
	var resp []byte
	if Hijacked() {
		resp, err = overHTTPS(context.Background(), msg)
	} else {
		resp, err = overUDP(server, msg, 3*time.Second)
	}
	if err != nil {
		return Answer{}, err
	}
	var p dnsmessage.Parser
	h, err := p.Start(resp)
	if err != nil {
		return Answer{}, err
	}
	_ = p.SkipAllQuestions()
	ans, err := p.AllAnswers()
	if err != nil {
		return Answer{}, err
	}
	return Answer{Authentic: h.AuthenticData, Records: ans}, nil
}

func build(name string, t dnsmessage.Type) ([]byte, error) {
	n, err := dnsmessage.NewName(strings.TrimSuffix(name, ".") + ".")
	if err != nil {
		return nil, err
	}
	b := dnsmessage.NewBuilder(nil, dnsmessage.Header{ID: uint16(rand.IntN(1 << 16)), RecursionDesired: true})
	b.EnableCompression()
	if err := b.StartQuestions(); err != nil {
		return nil, err
	}
	if err := b.Question(dnsmessage.Question{Name: n, Type: t, Class: dnsmessage.ClassINET}); err != nil {
		return nil, err
	}
	// EDNS с битом DO: без него резолвер не скажет, проверена ли подпись.
	if err := b.StartAdditionals(); err != nil {
		return nil, err
	}
	var opt dnsmessage.ResourceHeader
	if err := opt.SetEDNS0(1232, dnsmessage.RCodeSuccess, true); err != nil {
		return nil, err
	}
	if err := b.OPTResource(opt, dnsmessage.OPTResource{}); err != nil {
		return nil, err
	}
	return b.Finish()
}

func overUDP(server string, msg []byte, limit time.Duration) ([]byte, error) {
	conn, err := net.DialTimeout("udp", server, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(limit))
	if _, err := conn.Write(msg); err != nil {
		return nil, err
	}
	buf := make([]byte, 4096)
	k, err := conn.Read(buf)
	if err != nil {
		return nil, err
	}
	return buf[:k], nil
}

// Query спрашивает резолверы по очереди, пока один не ответит.
func Query(name string, t dnsmessage.Type) (Answer, error) {
	var last error
	for _, s := range Resolvers {
		a, err := query(s, name, t)
		if err == nil {
			return a, nil
		}
		last = err
	}
	if last == nil {
		last = errors.New("нет резолверов")
	}
	return Answer{}, last
}

// ParseCAA разбирает данные записи CAA: флаги, длина метки, метка, значение.
func ParseCAA(data []byte) (tag, value string, ok bool) {
	if len(data) < 2 {
		return "", "", false
	}
	n := int(data[1])
	if len(data) < 2+n {
		return "", "", false
	}
	return string(data[2 : 2+n]), string(data[2+n:]), true
}

// CAA: записи вида "issue letsencrypt.org"; пусто - любой центр может выпустить сертификат.
func CAA(name string) ([]string, error) {
	a, err := Query(name, typeCAA)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, r := range a.Records {
		u, ok := r.Body.(*dnsmessage.UnknownResource)
		if !ok || r.Header.Type != typeCAA {
			continue
		}
		if tag, val, ok := ParseCAA(u.Data); ok {
			out = append(out, tag+" "+val)
		}
	}
	return out, nil
}

// Signed: подписана ли зона и проверил ли подпись резолвер.
func Signed(name string) (bool, error) {
	a, err := Query(name, dnsmessage.TypeSOA)
	if err != nil {
		return false, err
	}
	return a.Authentic, nil
}
