package inspect

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"golang.org/x/net/publicsuffix"
)

type Domain struct {
	Registrar   string    `json:"registrar,omitempty"`
	Created     time.Time `json:"created,omitzero"`
	Expires     time.Time `json:"expires,omitzero"`
	Nameservers []string  `json:"nameservers,omitempty"`
	Source      string    `json:"source"`
}

var httpClient = &http.Client{Timeout: Timeout}

// Registration: RDAP, а для зон без RDAP (например .ru) whois по порту 43.
func Registration(domain string) (*Domain, error) {
	if d, err := rdap(domain); err == nil {
		return d, nil
	}
	text, err := whois(domain)
	if err != nil {
		return nil, err
	}
	d := ParseWhois(text)
	if d.Registrar == "" && d.Expires.IsZero() && len(d.Nameservers) == 0 {
		return nil, errors.New("whois не дал данных")
	}
	return d, nil
}

func rdap(domain string) (*Domain, error) {
	resp, err := httpClient.Get("https://rdap.org/domain/" + domain)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, errors.New("rdap: " + resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	return ParseRDAP(data)
}

// ParseRDAP достаёт регистратора, даты регистрации и окончания, NS из ответа RDAP.
func ParseRDAP(data []byte) (*Domain, error) {
	var r struct {
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
		return nil, err
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
		d.Nameservers = append(d.Nameservers, strings.ToLower(n.Name))
	}
	for _, e := range r.Entities {
		for _, role := range e.Roles {
			if role == "registrar" {
				d.Registrar = vcardName(e.Vcard)
			}
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
			return s
		}
	}
	return ""
}

func whois(domain string) (string, error) {
	tld := domain[strings.LastIndex(domain, ".")+1:]
	iana, err := whoisQuery("whois.iana.org", tld)
	if err != nil {
		return "", err
	}
	server := ""
	for line := range strings.SplitSeq(iana, "\n") {
		k, v, ok := strings.Cut(line, ":")
		if ok && (strings.TrimSpace(k) == "whois" || strings.TrimSpace(k) == "refer") && strings.TrimSpace(v) != "" {
			server = strings.TrimSpace(v)
			break
		}
	}
	if server == "" {
		return "", errors.New("нет whois-сервера для ." + tld)
	}
	return whoisQuery(server, domain)
}

func whoisQuery(server, q string) (string, error) {
	c, err := net.DialTimeout("tcp", net.JoinHostPort(server, "43"), Timeout)
	if err != nil {
		return "", err
	}
	defer func() { _ = c.Close() }()
	_ = c.SetDeadline(time.Now().Add(Timeout))
	if _, err := io.WriteString(c, q+"\r\n"); err != nil {
		return "", err
	}
	data, err := io.ReadAll(io.LimitReader(c, 1<<20))
	return string(data), err
}

var whoisDates = []string{time.RFC3339, "2006-01-02T15:04:05Z", "2006-01-02", "2006.01.02", "02-Jan-2006"}

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
		v = strings.TrimSpace(v)
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
