package dnsq

import (
	"net/netip"
	"reflect"
	"testing"

	"golang.org/x/net/dns/dnsmessage"
)

const (
	typeA     = dnsmessage.TypeA
	typeNS    = dnsmessage.TypeNS
	typeCNAME = dnsmessage.TypeCNAME
	typeSOA   = dnsmessage.TypeSOA
	nameError = dnsmessage.RCodeNameError
)

// Кто отвечает: сервер зоны сам, резолвер или сервер, который отсылает дальше.
var (
	fromZone     = dnsmessage.Header{Response: true, Authoritative: true}
	fromResolver = dnsmessage.Header{Response: true, RecursionDesired: true, RecursionAvailable: true}
	referring    = dnsmessage.Header{Response: true}

	soaCom   = []rec{{"example.com", typeSOA, ""}}
	soaNet   = []rec{{"example.net", typeSOA, ""}}
	childNS  = []rec{{"es.example.com", typeNS, "ns2.host.example.net"}, {"es.example.com", typeNS, "ns1.host.example.net"}}
	childIPs = []rec{{"ns1.host.example.net", typeA, "198.51.100.53"}, {"ns2.host.example.net", typeA, "203.0.113.53"}}
)

// rec: запись ответа; data - цель CNAME и NS или адрес A.
type rec struct {
	name string
	kind dnsmessage.Type
	data string
}

func (r rec) add(b *dnsmessage.Builder) error {
	h := dnsmessage.ResourceHeader{Name: dnsmessage.MustNewName(r.name + "."), Class: dnsmessage.ClassINET, TTL: 300}
	switch r.kind {
	case typeA:
		return b.AResource(h, dnsmessage.AResource{A: netip.MustParseAddr(r.data).As4()})
	case typeCNAME:
		return b.CNAMEResource(h, dnsmessage.CNAMEResource{CNAME: dnsmessage.MustNewName(r.data + ".")})
	case typeNS:
		return b.NSResource(h, dnsmessage.NSResource{NS: dnsmessage.MustNewName(r.data + ".")})
	}
	return b.SOAResource(h, dnsmessage.SOAResource{NS: h.Name, MBox: h.Name})
}

// message: сообщение с вопросом и записями по разделам: ответ, authority, additional.
func message(t *testing.T, h dnsmessage.Header, name string, kind dnsmessage.Type, sections ...[]rec) []byte {
	t.Helper()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	b := dnsmessage.NewBuilder(nil, h)
	b.EnableCompression()
	must(b.StartQuestions())
	must(b.Question(dnsmessage.Question{Name: dnsmessage.MustNewName(name + "."), Type: kind, Class: dnsmessage.ClassINET}))
	start := []func() error{b.StartAnswers, b.StartAuthorities, b.StartAdditionals}
	for i, list := range sections {
		must(start[i]())
		for _, r := range list {
			must(r.add(&b))
		}
	}
	msg, err := b.Finish()
	must(err)
	return msg
}

func coded(h dnsmessage.Header, code dnsmessage.RCode) dnsmessage.Header {
	h.RCode = code
	return h
}

// Вершина зоны по ответу резолвера на вопрос SOA: пусто значит "отрезать метку и спросить снова".
func TestReplyApex(t *testing.T) {
	cases := []struct {
		about string
		name  string
		msg   []byte
		want  string
	}{
		{"имя и есть вершина", "example.com", message(t, fromResolver, "example.com", typeSOA, soaCom), "example.com"},
		{"имени нет, SOA зоны в authority", "a.b.example.com",
			message(t, coded(fromResolver, nameError), "a.b.example.com", typeSOA, nil, soaCom), "example.com"},
		{"имя есть, записи нет", "mg.example.com", message(t, fromResolver, "mg.example.com", typeSOA, nil, soaCom), "example.com"},
		{"владелец SOA в другом регистре", "www.example.com",
			message(t, fromResolver, "www.example.com", typeSOA, nil, []rec{{"Example.COM", typeSOA, ""}}), "example.com"},
		{"CNAME увёл в чужую зону", "docs.example.com",
			message(t, fromResolver, "docs.example.com", typeSOA, []rec{{"docs.example.com", typeCNAME, "docs.cdn.example.net"}}, soaNet), ""},
		{"CNAME внутри своей зоны", "www.example.com",
			message(t, fromResolver, "www.example.com", typeSOA, []rec{{"www.example.com", typeCNAME, "example.com"}, soaCom[0]}), ""},
		{"чужой SOA без CNAME", "shop.example.com", message(t, fromResolver, "shop.example.com", typeSOA, nil, soaNet), ""},
		{"SOA зоны с похожим хвостом", "www.badexample.com", message(t, fromResolver, "www.badexample.com", typeSOA, nil, soaCom), ""},
		{"пустой ответ без SOA", "es.example.com", message(t, fromResolver, "es.example.com", typeSOA), ""},
		{"резолвер не дождался серверов", "es.example.com",
			message(t, coded(fromResolver, dnsmessage.RCodeServerFailure), "es.example.com", typeSOA), ""},
		{"SOA корня", "host.example", message(t, coded(fromResolver, nameError), "host.example", typeSOA, nil, []rec{{"", typeSOA, ""}}), ""},
	}
	for _, c := range cases {
		r, err := parseReply(c.msg)
		if err != nil {
			t.Errorf("%s: %v", c.about, err)
			continue
		}
		if got := r.apex(c.name); got != c.want {
			t.Errorf("%s: вершина %q, ждали %q", c.about, got, c.want)
		}
	}
}

// Отсылка к дочерней зоне: сервер вершины не отвечает сам, а называет серверы зоны между вершиной и именем.
func TestReplyChild(t *testing.T) {
	child := Zone{Name: "es.example.com", Servers: []ZoneServer{
		{Name: "ns1.host.example.net", Addrs: []string{"198.51.100.53"}},
		{Name: "ns2.host.example.net", Addrs: []string{"203.0.113.53"}},
	}}
	bare := Zone{Name: "es.example.com", Servers: []ZoneServer{{Name: "ns1.host.example.net"}, {Name: "ns2.host.example.net"}}}
	odd := append([]rec{{"es.example.com", typeNS, "ns\x1b[2J.example.net"}}, childNS...)
	apexNS := []rec{{"example.com", typeNS, "ns1.dns.example.net"}}
	cases := []struct {
		about string
		name  string
		msg   []byte
		want  Zone
		ok    bool
	}{
		{"имя - дочерняя зона", "es.example.com", message(t, referring, "es.example.com", typeA, nil, childNS, childIPs), child, true},
		{"имя глубже дочерней зоны", "x.es.example.com", message(t, referring, "x.es.example.com", typeA, nil, childNS, childIPs), child, true},
		{"адреса серверов не приложены", "es.example.com", message(t, referring, "es.example.com", typeA, nil, childNS), bare, true},
		{"имя сервера с управляющими символами", "es.example.com", message(t, referring, "es.example.com", typeA, nil, odd), bare, true},
		{"сервер отвечает сам, NS зоны в authority", "mail.example.com",
			message(t, fromZone, "mail.example.com", typeA, []rec{{"mail.example.com", typeA, "192.0.2.25"}}, apexNS), Zone{}, false},
		{"имени нет", "absent.example.com", message(t, coded(fromZone, nameError), "absent.example.com", typeA, nil, soaCom), Zone{}, false},
		{"отсылка к самой вершине", "www.example.com", message(t, referring, "www.example.com", typeA, nil, apexNS), Zone{}, false},
		{"отсылка вверх, к корню", "www.example.com",
			message(t, referring, "www.example.com", typeA, nil, []rec{{"", typeNS, "a.root.example.net"}}), Zone{}, false},
		{"отсылка в сторону", "www.example.com", message(t, referring, "www.example.com", typeA, nil, childNS), Zone{}, false},
		{"отказ", "es.example.com", message(t, coded(referring, dnsmessage.RCodeRefused), "es.example.com", typeA, nil, childNS), Zone{}, false},
		{"перехват: за сервер ответил резолвер", "es.example.com",
			message(t, fromResolver, "es.example.com", typeA, []rec{{"es.example.com", typeA, "192.0.2.80"}}), Zone{}, false},
	}
	for _, c := range cases {
		r, err := parseReply(c.msg)
		if err != nil {
			t.Errorf("%s: %v", c.about, err)
			continue
		}
		got, ok := r.child("example.com", c.name)
		if ok != c.ok || !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: %+v %v, ждали %+v %v", c.about, got, ok, c.want, c.ok)
		}
	}
}

// Годность сервера - по биту AA в ответе на вопрос SOA без рекурсии, а не по тому, чьё имя стоит у SOA.
func TestReplyAuthoritative(t *testing.T) {
	cases := []struct {
		about string
		msg   []byte
		want  bool
	}{
		{"сервер зоны", message(t, fromZone, "example.com", typeSOA, soaCom), true},
		{"дочерняя зона с SOA под именем родителя", message(t, fromZone, "es.example.com", typeSOA, nil, soaCom), true},
		{"перехват: ответил резолвер", message(t, fromResolver, "example.com", typeSOA, soaCom), false},
		{"сервер отсылает к другим", message(t, referring, "es.example.com", typeSOA, nil, childNS), false},
		{"отказ", message(t, coded(referring, dnsmessage.RCodeRefused), "example.com", typeSOA), false},
		{"сервер родителя такой зоны не знает", message(t, coded(fromZone, nameError), "es.example.com", typeSOA, nil, soaCom), false},
	}
	for _, c := range cases {
		r, err := parseReply(c.msg)
		if err != nil {
			t.Errorf("%s: %v", c.about, err)
			continue
		}
		if got := r.authoritative(); got != c.want {
			t.Errorf("%s: %v, ждали %v", c.about, got, c.want)
		}
	}
}

func TestParseReplyRejects(t *testing.T) {
	question := message(t, dnsmessage.Header{}, "example.com", typeSOA)
	whole := message(t, fromZone, "example.com", typeSOA, soaCom)
	for about, msg := range map[string][]byte{"вопрос вместо ответа": question, "обрыв посреди записи": whole[:len(whole)-5], "мусор": {1, 2, 3}} {
		if r, err := parseReply(msg); err == nil {
			t.Errorf("%s: принят как ответ %+v", about, r)
		}
	}
}

// Сервер после проверки адресов: молчащий остаётся в списке, чужой уходит, ответивший адрес идёт первым.
func TestSettle(t *testing.T) {
	two := []string{"192.0.2.1", "192.0.2.2"}
	cases := []struct {
		about  string
		addrs  []string
		states []state
		want   []string
		dead   bool
		keep   bool
	}{
		{"оба адреса отвечают", two, []state{alive, alive}, two, false, true},
		{"первый молчит", two, []state{silent, alive}, []string{"192.0.2.2", "192.0.2.1"}, false, true},
		{"оба молчат", two, []state{silent, silent}, two, true, true},
		{"один чужой, второй отвечает", two, []state{lame, alive}, []string{"192.0.2.2"}, false, true},
		{"один чужой, второй молчит", two, []state{lame, silent}, []string{"192.0.2.2"}, true, true},
		{"оба чужие", two, []state{lame, lame}, nil, false, false},
		{"адресов нет", nil, nil, nil, true, true},
	}
	for _, c := range cases {
		got, keep := settle(ZoneServer{Name: "ns1.example.net", Addrs: c.addrs}, c.states)
		if keep != c.keep || got.Dead != c.dead || !reflect.DeepEqual(got.Addrs, c.want) {
			t.Errorf("%s: %+v %v, ждали адреса %v, молчит %v, оставить %v", c.about, got, keep, c.want, c.dead, c.keep)
		}
	}
}

// Ответ принимается только на свой вопрос: тот же номер и то же имя без учёта регистра.
func TestMatches(t *testing.T) {
	answer := func(id uint16) dnsmessage.Header { return dnsmessage.Header{ID: id, Response: true} }
	query := message(t, dnsmessage.Header{ID: 7}, "www.example.com", typeA)
	refusal := dnsmessage.NewBuilder(nil, coded(answer(7), dnsmessage.RCodeRefused))
	bare, err := refusal.Finish()
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		about string
		resp  []byte
		want  bool
	}{
		{"свой ответ", message(t, answer(7), "www.example.com", typeA), true},
		{"имя в другом регистре", message(t, answer(7), "WwW.ExAmPlE.CoM", typeA), true},
		{"отказ без вопроса", bare, true},
		{"чужой номер", message(t, answer(8), "www.example.com", typeA), false},
		{"другое имя", message(t, answer(7), "mail.example.com", typeA), false},
		{"другой тип", message(t, answer(7), "www.example.com", typeNS), false},
		{"вопрос, а не ответ", query, false},
		{"мусор", []byte{0, 7, 0x80}, false},
	}
	for _, c := range cases {
		if got := matches(query, c.resp); got != c.want {
			t.Errorf("%s: %v, ждали %v", c.about, got, c.want)
		}
	}
	cut := answer(7)
	cut.Truncated = true
	if !truncated(message(t, cut, "www.example.com", typeA)) || truncated(message(t, answer(7), "www.example.com", typeA)) {
		t.Error("обрезанный ответ не отличён от целого")
	}
}
