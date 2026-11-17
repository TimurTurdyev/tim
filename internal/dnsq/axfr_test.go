package dnsq

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"

	"golang.org/x/net/dns/dnsmessage"
)

// answers: сообщение ответа на AXFR с длиной впереди; names - A-записи, soa - записи SOA по краям.
func answers(t *testing.T, aa bool, code dnsmessage.RCode, first, last bool, names ...string) []byte {
	t.Helper()
	zone := dnsmessage.MustNewName("example.com.")
	b := dnsmessage.NewBuilder(nil, dnsmessage.Header{Response: true, Authoritative: aa, RCode: code})
	if err := b.StartAnswers(); err != nil {
		t.Fatal(err)
	}
	soa := func() {
		h := dnsmessage.ResourceHeader{Name: zone, Class: dnsmessage.ClassINET}
		if err := b.SOAResource(h, dnsmessage.SOAResource{NS: zone, MBox: zone}); err != nil {
			t.Fatal(err)
		}
	}
	if first {
		soa()
	}
	for _, n := range names {
		h := dnsmessage.ResourceHeader{Name: dnsmessage.MustNewName(n + "."), Class: dnsmessage.ClassINET}
		if err := b.AResource(h, dnsmessage.AResource{A: [4]byte{192, 0, 2, 1}}); err != nil {
			t.Fatal(err)
		}
	}
	if last {
		soa()
	}
	msg, err := b.Finish()
	if err != nil {
		t.Fatal(err)
	}
	return append(binary.BigEndian.AppendUint16(nil, uint16(len(msg))), msg...)
}

func TestReadZone(t *testing.T) {
	ok := dnsmessage.RCodeSuccess
	whole := answers(t, true, ok, true, true, "www.example.com", "mail.example.com", "www.example.com")
	two := append(answers(t, true, ok, true, false, "www.example.com"), answers(t, true, ok, false, true, "vpn.example.com")...)
	cases := []struct {
		name    string
		stream  []byte
		records int
		names   int
		cut     bool
		err     error
	}{
		{"зона целиком", whole, 5, 2, false, nil},
		{"в двух сообщениях", two, 4, 2, false, nil},
		{"обрыв посреди зоны", two[:len(two)-20], 2, 1, true, nil},
		{"отказ", answers(t, true, dnsmessage.RCodeRefused, false, false), 0, 0, false, ErrRefused},
		{"пустой ответ", answers(t, true, ok, false, false), 0, 0, false, ErrRefused},
		{"только SOA", answers(t, true, ok, true, true), 0, 0, false, ErrRefused},
		{"обрыв сразу", nil, 0, 0, false, ErrRefused},
		{"обрыв после SOA", answers(t, true, ok, true, false), 0, 0, false, ErrRefused},
		{"ответил посредник", answers(t, false, ok, true, true, "www.example.com"), 0, 0, false, ErrNotZone},
		{"не зона", answers(t, true, ok, false, false, "www.example.com"), 0, 0, false, ErrRefused},
		{"мусор", []byte{0, 4, 1, 2, 3, 4}, 0, 0, false, ErrRefused},
	}
	for _, c := range cases {
		got, err := ReadZone(bytes.NewReader(c.stream))
		if !errors.Is(err, c.err) || got.Records != c.records || len(got.Names) != c.names || got.Cut != c.cut {
			t.Errorf("%s: %+v %v", c.name, got, err)
		}
	}
}
