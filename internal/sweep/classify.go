package sweep

import (
	"errors"
	"fmt"
	"net/netip"
	"strings"

	"golang.org/x/net/dns/dnsmessage"

	"github.com/TimurTurdyev/tim/internal/ui"
)

// facts: что нашлось в разделах ответа.
type facts struct {
	records int
	alias   string
	aliased bool
	ips     []string
	ptr     string
	soa     bool
	// zone: владелец NS из раздела authority - граница зоны при отсылке
	zone string
}

func (f *facts) read(p *dnsmessage.Parser, name string) error {
	if err := p.SkipAllQuestions(); err != nil {
		return err
	}
	if err := f.answers(p, name); err != nil {
		return err
	}
	return f.authority(p)
}

func (f *facts) answers(p *dnsmessage.Parser, name string) error {
	for {
		h, err := p.AnswerHeader()
		if errors.Is(err, dnsmessage.ErrSectionDone) {
			return nil
		}
		if err != nil {
			return err
		}
		f.records++
		if err := f.answer(p, h, name); err != nil {
			return err
		}
	}
}

func (f *facts) answer(p *dnsmessage.Parser, h dnsmessage.ResourceHeader, name string) error {
	switch h.Type {
	case dnsmessage.TypeCNAME:
		r, err := p.CNAMEResource()
		if err != nil {
			return err
		}
		if sameName(h.Name.String(), name) {
			f.alias, f.aliased = plain(r.CNAME), true
		}
	case dnsmessage.TypeA:
		r, err := p.AResource()
		if err != nil {
			return err
		}
		f.ips = append(f.ips, netip.AddrFrom4(r.A).String())
	case dnsmessage.TypeAAAA:
		r, err := p.AAAAResource()
		if err != nil {
			return err
		}
		f.ips = append(f.ips, netip.AddrFrom16(r.AAAA).String())
	case dnsmessage.TypePTR:
		r, err := p.PTRResource()
		if err != nil {
			return err
		}
		if f.ptr == "" {
			f.ptr = plain(r.PTR)
		}
	default:
		return p.SkipAnswer()
	}
	return nil
}

func (f *facts) authority(p *dnsmessage.Parser) error {
	for {
		h, err := p.AuthorityHeader()
		if errors.Is(err, dnsmessage.ErrSectionDone) {
			return nil
		}
		if err != nil {
			return err
		}
		switch h.Type {
		case dnsmessage.TypeSOA:
			f.soa = true
		case dnsmessage.TypeNS:
			if f.zone = plain(h.Name); f.zone == "" {
				f.zone = "."
			}
		}
		if err := p.SkipAuthority(); err != nil {
			return err
		}
	}
}

// verdict: порядок проверок важен - обрезанный ответ ограничителя и CNAME в никуда несут код NXDOMAIN.
func (f *facts) verdict(h dnsmessage.Header, asked dnsmessage.Type, a *Answer) {
	// обратная запись, найденная через CNAME (RFC 2317), - это ответ, а не псевдоним
	found := asked == dnsmessage.TypePTR && f.ptr != ""
	switch {
	case f.aliased && !found:
		a.Kind, a.Target, a.IPs = CNAME, f.alias, f.ips
	case f.records > 0 && (h.RCode == dnsmessage.RCodeSuccess || h.RCode == dnsmessage.RCodeNameError):
		a.Kind, a.IPs, a.Target = Addr, f.ips, f.ptr
	case h.RCode == dnsmessage.RCodeNameError:
		a.Kind = NX
	case h.RCode != dnsmessage.RCodeSuccess:
		a.Kind = Fail
	case !h.Authoritative && f.zone != "" && !f.soa:
		// отсылка вверх или вбок: сервер зону не обслуживает
		a.Kind = Fail
		if within(a.Name, f.zone) {
			a.Kind, a.Zone = Refer, f.zone
		}
	default:
		a.Kind = NoData
	}
}

// Classify разбирает ответ на вопрос об имени name; truncated - ответ обрезан, коду в нём верить нельзя.
func Classify(msg []byte, name string) (a Answer, truncated bool) {
	a.Name = name
	var p dnsmessage.Parser
	h, err := p.Start(msg)
	if err != nil || !h.Response {
		return a, false
	}
	q, err := p.Question()
	if err != nil || !sameName(q.Name.String(), name) {
		return a, false
	}
	if h.Truncated {
		return a, true
	}
	var f facts
	if f.read(&p, name) != nil {
		a.Kind = Fail
		return a, false
	}
	f.verdict(h, q.Type, &a)
	return a, false
}

// buildQuery: запрос без EDNS - так его шлют и напрямую, и по DoH.
func buildQuery(buf []byte, id uint16, name string, asked dnsmessage.Type, recurse bool) ([]byte, error) {
	n, err := dnsmessage.NewName(strings.TrimSuffix(name, ".") + ".")
	if err != nil {
		return nil, fmt.Errorf("имя не годится для запроса: %w", err)
	}
	b := dnsmessage.NewBuilder(buf[:0], dnsmessage.Header{ID: id, RecursionDesired: recurse})
	if err := b.StartQuestions(); err != nil {
		return nil, fmt.Errorf("запрос не собрался: %w", err)
	}
	if err := b.Question(dnsmessage.Question{Name: n, Type: asked, Class: dnsmessage.ClassINET}); err != nil {
		return nil, fmt.Errorf("имя не годится для запроса: %w", err)
	}
	msg, err := b.Finish()
	if err != nil {
		return nil, fmt.Errorf("запрос не собрался: %w", err)
	}
	return msg, nil
}

// question: номер и имя вопроса из ответа.
func question(msg []byte) (id uint16, name string, ok bool) {
	var p dnsmessage.Parser
	h, err := p.Start(msg)
	if err != nil || !h.Response {
		return 0, "", false
	}
	q, err := p.Question()
	if err != nil {
		return 0, "", false
	}
	return h.ID, q.Name.String(), true
}

// packable: имя влезает в запрос - метки от 1 до 63 байт, всё имя до 253.
func packable(name string) bool {
	name = strings.TrimSuffix(name, ".")
	if name == "" || len(name) > 253 {
		return false
	}
	for label := range strings.SplitSeq(name, ".") {
		if label == "" || len(label) > 63 {
			return false
		}
	}
	return true
}

// plain: имя из чужого ответа - без точки в конце, строчными, без управляющих символов.
func plain(n dnsmessage.Name) string {
	return ui.Plain(strings.ToLower(strings.TrimSuffix(n.String(), ".")))
}
