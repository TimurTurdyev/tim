package inspect

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestRegistryBase(t *testing.T) {
	list, err := parseRegistries([]byte(sample(t, "rdap-dns.json")))
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct{ domain, base string }{
		{"example.com", "https://rdap.registry.example/com/v1/"},
		{"EXAMPLE.NET.", "https://rdap.registry.example/com/v1/"},
		{"example.org", "https://rdap.example.org/rdap/"},
		{"example.xn--80akhbyknj4f", "https://rdap.example.net/"},
		{"пример.испытание", "https://rdap.example.net/"},
		{"site.example", "http://rdap.nic.example/"},
		{"example.test", "https://rdap.example.test/no-slash/"},
		{"example.invalid", ""},
		{"example.localhost", ""},
		{"com", "https://rdap.registry.example/com/v1/"},
	}
	for _, c := range cases {
		name, err := asciiName(c.domain)
		if err != nil {
			t.Errorf("%s: %v", c.domain, err)
			continue
		}
		if got := list.base(name); got != c.base {
			t.Errorf("%s: адрес %q, нужен %q", c.domain, got, c.base)
		}
	}
}

func TestParseRegistriesRejects(t *testing.T) {
	for _, data := range []string{`не json`, `{}`, `{"services":[]}`, `{"services":[[["com"]]]}`,
		`{"services":[[["com"],["ftp://rdap.example.com/"]]]}`} {
		if list, err := parseRegistries([]byte(data)); err == nil {
			t.Errorf("%s: разобран как список %v", data, list)
		}
	}
}

func TestBuiltinRegistries(t *testing.T) {
	list, err := builtinRegistries()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) < 1000 {
		t.Errorf("зон %d, в списке IANA их больше тысячи", len(list))
	}
	for zone, base := range list {
		if !strings.HasPrefix(base, "http") || !strings.HasSuffix(base, "/") {
			t.Errorf("%s: адрес %q", zone, base)
		}
	}
	if list.base("example.com") == "" || list.base("example.org") == "" || list.base("example.localhost") != "" {
		t.Errorf("com %q, org %q, localhost %q", list.base("example.com"), list.base("example.org"),
			list.base("example.localhost"))
	}
}

func TestAsciiNameRejects(t *testing.T) {
	for _, domain := range []string{"", ".", "example.com\r\nexample.net", "exa mple.com", "exa_mple.com"} {
		if name, err := asciiName(domain); err == nil {
			t.Errorf("%q принято как %q", domain, name)
		}
	}
}

func TestParseRDAPSamples(t *testing.T) {
	cases := []struct {
		file, registrar, created, expires string
		ns                                []string
	}{
		{"rdap-registry-com.json", "Example Registrar, Inc.", "2007-07-04T01:13:38Z", "2029-07-04T01:13:38Z",
			[]string{"ns1.example.net", "ns2.example.net"}},
		{"rdap-registry-org.json", "Example Registrar SAS", "1995-03-27T05:00:00.545Z", "2033-03-28T05:00:00Z",
			[]string{"ns2.example.net", "ns1.example.net"}},
	}
	for _, c := range cases {
		d, err := ParseRDAP([]byte(sample(t, c.file)))
		if err != nil {
			t.Errorf("%s: %v", c.file, err)
			continue
		}
		created, expires := d.Created.Format(time.RFC3339Nano), d.Expires.Format(time.RFC3339Nano)
		if d.Registrar != c.registrar || created != c.created || expires != c.expires || d.Source != "rdap" {
			t.Errorf("%s: %q %s %s %s", c.file, d.Registrar, created, expires, d.Source)
		}
		if !slices.Equal(d.Nameservers, c.ns) {
			t.Errorf("%s: ns %v", c.file, d.Nameservers)
		}
	}
}

func TestParseRDAPRejects(t *testing.T) {
	for _, data := range []string{
		`{"rdapConformance":["rdap_level_0"],"lang":"en","errorCode":404,"title":"No RDAP service is available"}`,
		`{"objectClassName":"nameserver","ldhName":"ns1.example.net"}`,
		`<html>502 Bad Gateway</html>`,
	} {
		if d, err := ParseRDAP([]byte(data)); err == nil {
			t.Errorf("%s: разобран как домен %+v", data, d)
		}
	}
}

func TestWhoisServer(t *testing.T) {
	if got := whoisServer(sample(t, "whois-iana-refer.txt")); got != "whois.nic.example" {
		t.Errorf("зона с whois: сервер %q", got)
	}
	if got := whoisServer(sample(t, "whois-iana-no-whois.txt")); got != "" {
		t.Errorf("зона без whois: сервер %q", got)
	}
}

func TestRegistrationOrder(t *testing.T) {
	const base = "https://rdap.example.net/"
	down := errors.New("сервер не ответил")
	answer := "registrar: Example Registrar\nnserver: ns1.example.net.\n"
	cases := []struct {
		name            string
		base            string
		registry, proxy error
		own             bool
		whois           string
		whoisErr        error
		calls           []string
		source          string
		err             error
	}{
		{name: "реестр ответил", base: base, calls: []string{"реестр"}, source: "rdap"},
		{name: "реестр не знает домена", base: base, registry: errNotFound, calls: []string{"реестр"},
			err: ErrNoDomain},
		{name: "реестр лежит", base: base, registry: down, calls: []string{"реестр", "rdap.org"},
			source: "rdap.org"},
		{name: "RDAP лежит", base: base, registry: down, proxy: down, whois: answer,
			calls: []string{"реестр", "rdap.org", "whois"}, source: "whois"},
		{name: "rdap.org не знает зоны", base: base, registry: down, proxy: errNotFound, whois: answer,
			calls: []string{"реестр", "rdap.org", "whois"}, source: "whois"},
		{name: "без посредника реестр лежит", base: base, own: true, registry: down, whois: answer,
			calls: []string{"реестр", "whois"}, source: "whois"},
		{name: "зоны нет в списке", whois: answer, calls: []string{"whois"}, source: "whois"},
		{name: "зоны нет в списке, whois пуст", whois: "No match for domain\n", calls: []string{"whois"},
			err: errNoData},
		{name: "нет ни RDAP, ни whois", whoisErr: errNoWhois, calls: []string{"whois"}, err: errNoWhois},
		{name: "только RDAP, и он лежит", base: base, registry: down, proxy: down, whoisErr: errNoWhois,
			calls: []string{"реестр", "rdap.org", "whois"}, err: down},
	}
	for _, c := range cases {
		var calls []string
		ask := sources{
			rdap: func(_ context.Context, server, _ string) (*Domain, error) {
				step, err := "реестр", c.registry
				if server == rdapProxy {
					step, err = "rdap.org", c.proxy
				}
				calls = append(calls, step)
				if err != nil {
					return nil, err
				}
				return &Domain{Source: "rdap"}, nil
			},
			whois: func(context.Context, string) (string, error) {
				calls = append(calls, "whois")
				return c.whois, c.whoisErr
			},
			own: c.own,
		}
		d, err := ask.registration(context.Background(), "example.com", c.base)
		if !slices.Equal(calls, c.calls) {
			t.Errorf("%s: шаги %v, нужны %v", c.name, calls, c.calls)
		}
		if !errors.Is(err, c.err) || (err == nil) != (c.err == nil) {
			t.Errorf("%s: ошибка %v, нужна %v", c.name, err, c.err)
		}
		if err == nil && d.Source != c.source {
			t.Errorf("%s: источник %q, нужен %q", c.name, d.Source, c.source)
		}
	}
}
