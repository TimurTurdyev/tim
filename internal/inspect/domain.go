package inspect

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"slices"
	"strings"
	"time"

	"golang.org/x/net/idna"
	"golang.org/x/net/publicsuffix"

	"github.com/TimurTurdyev/tim/internal/ui"
)

const (
	// rdapProxy: посредник, который сам находит реестр зоны и перенаправляет к нему.
	rdapProxy = "https://rdap.org/"
	// chainTimeout: вся цепочка Registration - список реестров, реестр, rdap.org и два запроса whois.
	chainTimeout = 5 * Timeout
)

var (
	httpClient = &http.Client{Timeout: Timeout}

	whoisDates = []string{time.RFC3339, "2006-01-02T15:04:05Z", "2006-01-02", "2006.01.02", "02-Jan-2006"}

	// ErrNoDomain: реестр зоны ответил, что такого домена у него нет.
	ErrNoDomain = errors.New("домен не зарегистрирован")

	errNoWhois = errors.New("whois-сервера нет")
	errNoData  = errors.New("whois не дал данных")
)

type Domain struct {
	Registrar   string    `json:"registrar,omitempty"`
	Created     time.Time `json:"created,omitzero"`
	Expires     time.Time `json:"expires,omitzero"`
	Nameservers []string  `json:"nameservers,omitempty"`
	Source      string    `json:"source"`
}

// sources: чем спросить сервер RDAP и whois; порядок обхода проверяется без сети.
type sources struct {
	rdap  func(ctx context.Context, base, name string) (*Domain, error)
	whois func(ctx context.Context, name string) (string, error)
	own   bool // без посредника rdap.org: только реестр зоны
}

// registration: реестр по адресу base, при его отказе rdap.org и whois; base пуст - сразу whois.
func (s sources) registration(ctx context.Context, name, base string) (*Domain, error) {
	if base == "" {
		return s.viaWhois(ctx, name, nil)
	}
	d, err := s.rdap(ctx, base, name)
	switch {
	case err == nil:
		return d, nil
	case errors.Is(err, errNotFound):
		return nil, fmt.Errorf("реестр зоны .%s не знает %s: %w", lastLabel(name), name, ErrNoDomain)
	}
	if !s.own {
		if viaProxy, proxyErr := s.rdap(ctx, rdapProxy, name); proxyErr == nil {
			viaProxy.Source = "rdap.org"
			return viaProxy, nil
		}
	}
	return s.viaWhois(ctx, name, err)
}

// viaWhois: rdapErr - почему перед этим не ответил RDAP; nil - у зоны его нет.
func (s sources) viaWhois(ctx context.Context, name string, rdapErr error) (*Domain, error) {
	text, err := s.whois(ctx, name)
	switch {
	case errors.Is(err, errNoWhois) && rdapErr != nil:
		return nil, fmt.Errorf("у зоны .%s только RDAP, и он не ответил (%w): данных о регистрации нет",
			lastLabel(name), rdapErr)
	case err != nil:
		return nil, err
	}
	d := ParseWhois(text)
	if d.Registrar == "" && d.Expires.IsZero() && len(d.Nameservers) == 0 {
		return nil, errNoData
	}
	return d, nil
}

// Registration: реестр зоны из списка IANA, при его отказе rdap.org и whois; зоны нет в списке - сразу whois.
func Registration(domain string) (*Domain, error) {
	return registration(domain, false)
}

// RegistrationOwn: то же без rdap.org - домен уходит только реестру своей зоны.
func RegistrationOwn(domain string) (*Domain, error) {
	return registration(domain, true)
}

func registration(domain string, own bool) (*Domain, error) {
	name, err := asciiName(domain)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), chainTimeout)
	defer cancel()
	return sources{rdap: rdap, whois: whois, own: own}.registration(ctx, name, currentRegistries().base(name))
}

// asciiName: имя для запроса - в нижнем регистре, без точки на конце, IDN в виде xn--.
func asciiName(domain string) (string, error) {
	name, err := idna.Lookup.ToASCII(strings.TrimSuffix(strings.TrimSpace(domain), "."))
	if err != nil {
		return "", fmt.Errorf("имя домена %q не годится для запроса: %w", domain, err)
	}
	if name == "" {
		return "", errors.New("имя домена пустое")
	}
	return name, nil
}

// rdap спрашивает домен у сервера RDAP: base с "/" на конце, имя в ASCII.
func rdap(ctx context.Context, base, name string) (*Domain, error) {
	data, err := getJSON(ctx, base+"domain/"+name)
	if err != nil {
		return nil, err
	}
	return ParseRDAP(data)
}

// ParseRDAP достаёт регистратора, даты регистрации и окончания, NS из ответа RDAP о домене.
func ParseRDAP(data []byte) (*Domain, error) {
	var r struct {
		Class  string `json:"objectClassName"`
		Events []struct {
			Action string `json:"eventAction"`
			Date   string `json:"eventDate"`
		} `json:"events"`
		Nameservers []struct {
			Name string `json:"ldhName"`
		} `json:"nameservers"`
		Entities []struct {
			Roles []string        `json:"roles"`
			Vcard json.RawMessage `json:"vcardArray"`
		} `json:"entities"`
	}
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("ответ RDAP не разобран: %w", err)
	}
	if r.Class != "domain" {
		return nil, errors.New("в ответе RDAP не домен")
	}
	d := &Domain{Source: "rdap"}
	for _, e := range r.Events {
		t, err := time.Parse(time.RFC3339, e.Date)
		if err != nil {
			continue
		}
		switch e.Action {
		case "registration":
			d.Created = t
		case "expiration":
			d.Expires = t
		}
	}
	for _, n := range r.Nameservers {
		if name := strings.ToLower(strings.TrimSuffix(ui.Plain(n.Name), ".")); name != "" {
			d.Nameservers = append(d.Nameservers, name)
		}
	}
	for _, e := range r.Entities {
		if d.Registrar == "" && slices.Contains(e.Roles, "registrar") {
			d.Registrar = vcardName(e.Vcard)
		}
	}
	return d, nil
}

// vcardName: ["vcard", [["fn", {}, "text", "Имя"], ...]].
func vcardName(raw json.RawMessage) string {
	var v []any
	if json.Unmarshal(raw, &v) != nil || len(v) < 2 {
		return ""
	}
	props, _ := v[1].([]any)
	for _, p := range props {
		f, _ := p.([]any)
		if len(f) >= 4 && f[0] == "fn" {
			s, _ := f[3].(string)
			return ui.Plain(s)
		}
	}
	return ""
}

func whois(ctx context.Context, name string) (string, error) {
	zone := lastLabel(name)
	iana, err := whoisQuery(ctx, "whois.iana.org", zone)
	if err != nil {
		return "", err
	}
	server := whoisServer(iana)
	if server == "" {
		return "", fmt.Errorf("зона .%s: %w", zone, errNoWhois)
	}
	return whoisQuery(ctx, server, name)
}

// whoisServer: сервер зоны из ответа whois.iana.org - строка whois или refer; у зон с одним RDAP обе пусты.
func whoisServer(iana string) string {
	for line := range strings.SplitSeq(iana, "\n") {
		k, v, ok := strings.Cut(line, ":")
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		if ok && (k == "whois" || k == "refer") && v != "" {
			return ui.Plain(v)
		}
	}
	return ""
}

func whoisQuery(ctx context.Context, server, q string) (string, error) {
	dialer := net.Dialer{Timeout: Timeout}
	c, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(server, "43"))
	if err != nil {
		return "", fmt.Errorf("whois-сервер %s недоступен: %w", server, err)
	}
	defer func() { _ = c.Close() }()
	deadline := time.Now().Add(Timeout)
	if limit, ok := ctx.Deadline(); ok && limit.Before(deadline) {
		deadline = limit
	}
	_ = c.SetDeadline(deadline)
	if _, err := io.WriteString(c, q+"\r\n"); err != nil {
		return "", fmt.Errorf("запрос к whois-серверу %s не ушёл: %w", server, err)
	}
	data, err := io.ReadAll(io.LimitReader(c, maxAnswer))
	if err != nil {
		return "", fmt.Errorf("ответ whois-сервера %s оборван: %w", server, err)
	}
	return string(data), nil
}

// ParseWhois понимает ключи gTLD и зоны .ru/.рф (registrar, created, paid-till, nserver).
func ParseWhois(text string) *Domain {
	d := &Domain{Source: "whois"}
	sc := bufio.NewScanner(strings.NewReader(text))
	for sc.Scan() {
		k, v, ok := strings.Cut(sc.Text(), ":")
		if !ok {
			continue
		}
		k = strings.ToLower(strings.TrimSpace(k))
		v = strings.TrimSpace(ui.Plain(v))
		if v == "" {
			continue
		}
		switch k {
		case "registrar":
			if d.Registrar == "" {
				d.Registrar = v
			}
		case "creation date", "created":
			d.Created = parseWhoisDate(v)
		case "registry expiry date", "registrar registration expiration date", "paid-till", "expiration date":
			if d.Expires.IsZero() {
				d.Expires = parseWhoisDate(v)
			}
		case "name server", "nserver":
			ns := strings.ToLower(strings.TrimSuffix(strings.Fields(v)[0], "."))
			d.Nameservers = append(d.Nameservers, ns)
		}
	}
	return d
}

func parseWhoisDate(s string) time.Time {
	for _, l := range whoisDates {
		if t, err := time.Parse(l, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

// Registrable: домен, который регистрируют (по таблице публичных суффиксов),
// для запроса RDAP и whois.
func Registrable(host string) string {
	host = strings.TrimSuffix(host, ".")
	if d, err := publicsuffix.EffectiveTLDPlusOne(host); err == nil {
		return d
	}
	parts := strings.Split(host, ".")
	if len(parts) <= 2 {
		return host
	}
	return strings.Join(parts[len(parts)-2:], ".")
}
