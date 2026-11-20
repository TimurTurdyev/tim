package dnsq

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"slices"
	"strings"

	"golang.org/x/net/dns/dnsmessage"

	"github.com/TimurTurdyev/tim/internal/par"
)

// maxDescent: сколько раз подряд идём по отсылке к дочерней зоне.
const maxDescent = 6

// Zone: ближайшая зона имени и её серверы.
type Zone struct {
	Name    string
	Servers []ZoneServer
	// Direct: серверы спрошены напрямую; false - порт 53 перехвачен, зону и серверы назвал резолвер.
	Direct bool
}

// ZoneServer: сервер зоны и его адреса IPv4, ответившие за зону - первыми.
type ZoneServer struct {
	Name  string
	Addrs []string
	// Dead: ни один адрес не ответил; сервер остаётся в списке - молчание бывает временным.
	Dead bool
}

// reply: что из ответа нужно для поиска зоны.
type reply struct {
	rcode    dnsmessage.RCode
	aa       bool
	answers  int
	cname    bool                // у спрошенного имени CNAME
	soa      string              // владелец SOA из ответа, а нет - из authority
	ns       []string            // NS спрошенного имени из ответа
	addrs    []string            // A из ответа
	refOwner string              // владелец NS из authority - граница зоны при отсылке
	refNS    []string            // сами NS из authority
	glue     map[string][]string // A из additional по именам
}

// apex: вершина зоны по ответу на вопрос SOA; пусто - отрезать метку слева и спросить снова.
func (r reply) apex(name string) string {
	if r.cname || r.soa == "" || !within(name, r.soa) {
		return ""
	}
	return r.soa
}

// child: отсылка к зоне между вершиной и именем - сервер не отвечает сам, а называет её серверы.
func (r reply) child(apex, name string) (Zone, bool) {
	refers := !r.aa && r.rcode == dnsmessage.RCodeSuccess && r.answers == 0 && r.soa == "" && len(r.refNS) > 0
	if !refers || r.refOwner == apex || !within(r.refOwner, apex) || !within(name, r.refOwner) {
		return Zone{}, false
	}
	return Zone{Name: r.refOwner, Servers: servers(r.refNS, r.glue)}, true
}

// authoritative: сервер отвечает за зону сам; через перехват и у чужого сервера бита AA нет.
func (r reply) authoritative() bool {
	return r.aa && r.rcode == dnsmessage.RCodeSuccess
}

func (r *reply) answer(name string, records []dnsmessage.Resource) {
	r.answers = len(records)
	for _, rec := range records {
		owner := lower(rec.Header.Name)
		switch body := rec.Body.(type) {
		case *dnsmessage.CNAMEResource:
			r.cname = r.cname || owner == name
		case *dnsmessage.SOAResource:
			r.soa = owner
		case *dnsmessage.NSResource:
			if host := lower(body.NS); owner == name && hostname(host) {
				r.ns = append(r.ns, host)
			}
		case *dnsmessage.AResource:
			r.addrs = append(r.addrs, netip.AddrFrom4(body.A).String())
		}
	}
	slices.Sort(r.addrs)
	r.addrs = slices.Compact(r.addrs)
}

func (r *reply) authority(records []dnsmessage.Resource) {
	for _, rec := range records {
		owner := lower(rec.Header.Name)
		switch body := rec.Body.(type) {
		case *dnsmessage.SOAResource:
			if r.soa == "" {
				r.soa = owner
			}
		case *dnsmessage.NSResource:
			if host := lower(body.NS); hostname(host) {
				r.refOwner = owner
				r.refNS = append(r.refNS, host)
			}
		}
	}
}

func (r *reply) additional(records []dnsmessage.Resource) {
	for _, rec := range records {
		body, ok := rec.Body.(*dnsmessage.AResource)
		if !ok {
			continue
		}
		if r.glue == nil {
			r.glue = map[string][]string{}
		}
		owner := lower(rec.Header.Name)
		r.glue[owner] = append(r.glue[owner], netip.AddrFrom4(body.A).String())
	}
}

// state: что адрес сервера ответил на вопрос SOA без рекурсии.
type state int

const (
	silent state = iota // ответа нет
	alive               // отвечает за зону сам
	lame                // ответил, но зону не обслуживает
)

// FindZone: ближайшая зона имени - вершина по SOA у резолвера, спуск по отсылкам её серверов, проверка каждого.
func FindZone(name string) (Zone, error) {
	name = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(name), "."))
	z, err := nearest(name)
	if err != nil {
		return Zone{}, err
	}
	z.Direct = !Hijacked()
	if !z.Direct {
		return z, nil
	}
	for range maxDescent {
		z.Servers = verify(z)
		child, ok := descend(z, name)
		if !ok {
			return z, nil
		}
		z = child
	}
	z.Servers = verify(z)
	return z, nil
}

// nearest: зона и серверы со слов резолвера; дочернюю зону с кривым SOA он не видит - её находит descend.
func nearest(name string) (Zone, error) {
	apex := ""
	for cur := name; cur != "" && apex == ""; cur = parent(cur) {
		r, err := recurse(cur, dnsmessage.TypeSOA)
		if err != nil {
			return Zone{}, fmt.Errorf("зона имени %q не найдена: %w", name, err)
		}
		apex = r.apex(cur)
	}
	if apex == "" {
		return Zone{}, fmt.Errorf("зона имени %q не найдена: записи SOA нет ни у него, ни выше", name)
	}
	r, err := recurse(apex, dnsmessage.TypeNS)
	if err != nil {
		return Zone{}, fmt.Errorf("серверы зоны %s не найдены: %w", apex, err)
	}
	if len(r.ns) == 0 {
		return Zone{}, fmt.Errorf("резолвер не назвал серверы зоны %s", apex)
	}
	return Zone{Name: apex, Servers: addressed(servers(r.ns, nil))}, nil
}

// recurse: вопрос резолверам по очереди; SERVFAIL - повод спросить следующего, а от всех подряд - это и есть ответ.
func recurse(name string, t dnsmessage.Type) (reply, error) {
	var failed []reply
	last := errNoResolvers
	for _, server := range Resolvers {
		msg, err := Exchange(server, name, t, true)
		if err != nil {
			last = err
			continue
		}
		r, err := parseReply(msg)
		if err != nil {
			last = err
			continue
		}
		if r.rcode != dnsmessage.RCodeServerFailure {
			return r, nil
		}
		failed = append(failed, r)
	}
	if len(failed) > 0 {
		return failed[0], nil
	}
	return reply{}, last
}

// addressed: серверу, чей адрес не пришёл вместе с именем, адрес ищет резолвер.
func addressed(list []ZoneServer) []ZoneServer {
	return par.Map(list, len(list), func(s ZoneServer) ZoneServer {
		if len(s.Addrs) > 0 {
			return s
		}
		if r, err := recurse(s.Name, dnsmessage.TypeA); err == nil {
			s.Addrs = r.addrs
		}
		return s
	})
}

// verify: каждый адрес отвечает на вопрос SOA вершины без рекурсии; годен тот, кто отвечает за зону сам.
func verify(z Zone) []ZoneServer {
	states := par.Map(z.Servers, len(z.Servers), func(s ZoneServer) []state {
		return par.Map(s.Addrs, len(s.Addrs), func(addr string) state { return check(addr, z.Name) })
	})
	var out []ZoneServer
	for i, s := range z.Servers {
		if kept, ok := settle(s, states[i]); ok {
			out = append(out, kept)
		}
	}
	return out
}

func check(addr, apex string) state {
	msg, err := Exchange(net.JoinHostPort(addr, "53"), apex, dnsmessage.TypeSOA, false)
	if err != nil {
		return silent
	}
	if r, err := parseReply(msg); err == nil && r.authoritative() {
		return alive
	}
	return lame
}

// descend: зона глубже, если серверы этой отсылают имя к дочерней.
func descend(z Zone, name string) (Zone, bool) {
	if name == z.Name {
		return Zone{}, false
	}
	for _, addr := range answering(z.Servers) {
		msg, err := Exchange(net.JoinHostPort(addr, "53"), name, dnsmessage.TypeA, false)
		if err != nil {
			continue
		}
		r, err := parseReply(msg)
		if err != nil {
			continue
		}
		child, ok := r.child(z.Name, name)
		if !ok {
			return Zone{}, false
		}
		child.Servers = addressed(child.Servers)
		child.Direct = true
		return child, true
	}
	return Zone{}, false
}

// parseReply: поля ответа, по которым ищется зона; имена - без точки в конце и строчными.
func parseReply(msg []byte) (reply, error) {
	var p dnsmessage.Parser
	h, err := p.Start(msg)
	if err != nil {
		return reply{}, fmt.Errorf("ответ не разобран: %w", err)
	}
	if !h.Response {
		return reply{}, errors.New("пришёл не ответ")
	}
	q, err := p.Question()
	if err != nil {
		return reply{}, errors.New("ответ без вопроса")
	}
	if err := p.SkipAllQuestions(); err != nil {
		return reply{}, fmt.Errorf("ответ не разобран: %w", err)
	}
	r := reply{rcode: h.RCode, aa: h.Authoritative}
	answers, err := p.AllAnswers()
	if err != nil {
		return reply{}, fmt.Errorf("ответ не разобран: %w", err)
	}
	r.answer(lower(q.Name), answers)
	authorities, err := p.AllAuthorities()
	if err != nil {
		return reply{}, fmt.Errorf("ответ не разобран: %w", err)
	}
	r.authority(authorities)
	additionals, err := p.AllAdditionals()
	if err != nil {
		return reply{}, fmt.Errorf("ответ не разобран: %w", err)
	}
	r.additional(additionals)
	return r, nil
}

// settle: сервер после проверки адресов - чужие адреса долой, годные вперёд; сервер из одних чужих не нужен.
func settle(s ZoneServer, states []state) (ZoneServer, bool) {
	var good, quiet []string
	for i, addr := range s.Addrs {
		switch states[i] {
		case alive:
			good = append(good, addr)
		case silent:
			quiet = append(quiet, addr)
		}
	}
	if len(s.Addrs) > 0 && len(good)+len(quiet) == 0 {
		return ZoneServer{}, false
	}
	return ZoneServer{Name: s.Name, Addrs: slices.Concat(good, quiet), Dead: len(good) == 0}, true
}

// servers: серверы по именам, по алфавиту и без повторов; адреса - какие пришли вместе с именами.
func servers(names []string, glue map[string][]string) []ZoneServer {
	names = slices.Compact(slices.Sorted(slices.Values(names)))
	out := make([]ZoneServer, 0, len(names))
	for _, name := range names {
		out = append(out, ZoneServer{Name: name, Addrs: glue[name]})
	}
	return out
}

// answering: по одному ответившему адресу от каждого живого сервера.
func answering(list []ZoneServer) []string {
	var out []string
	for _, s := range list {
		if !s.Dead && len(s.Addrs) > 0 {
			out = append(out, s.Addrs[0])
		}
	}
	return out
}

func within(name, zone string) bool {
	return zone != "" && (name == zone || strings.HasSuffix(name, "."+zone))
}

func parent(name string) string {
	_, rest, _ := strings.Cut(name, ".")
	return rest
}

func lower(n dnsmessage.Name) string {
	return strings.ToLower(strings.TrimSuffix(n.String(), "."))
}

// hostname: имя сервера приходит из чужого ответа; всё, что на имя хоста не похоже, отбрасывается.
func hostname(s string) bool {
	if s == "" || len(s) > 253 {
		return false
	}
	for _, c := range []byte(s) {
		letter := c >= 'a' && c <= 'z' || c >= '0' && c <= '9'
		if !letter && c != '-' && c != '_' && c != '.' {
			return false
		}
	}
	return true
}
