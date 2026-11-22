package servers

import (
	"slices"
	"testing"

	"github.com/TimurTurdyev/tim/internal/srvinfo"
)

func TestSiteNames(t *testing.T) {
	cases := []struct {
		name        string
		sites       []srvinfo.Site
		certs       []srvinfo.Cert
		names, wild []string
	}{
		{
			name: "обход nginx: имена, маска, регулярное выражение, точка впереди, адрес",
			sites: []srvinfo.Site{
				{Name: "example.com", Aliases: []string{"www.example.com"}},
				{Name: "api.example.com", Aliases: []string{"api-stage.example.com"}},
				{Name: "*.dev.example.com"},
				{Name: `~^(?<tenant>[a-z0-9-]+)\.shop\.example\.com$`},
				{Name: ".example.net"},
				{Name: "localhost", Aliases: []string{"192.0.2.10", "grafana.internal"}},
				{Name: "xn--e1afmkfd.example.com"},
			},
			names: []string{
				"api-stage.example.com", "api.example.com", "example.com", "example.net", "grafana.internal",
				"www.example.com", "xn--e1afmkfd.example.com",
			},
			wild: []string{"dev.example.com", "example.net"},
		},
		{
			name: "регистр, точка в конце, повторы",
			sites: []srvinfo.Site{
				{Name: "Example.COM.", Aliases: []string{"example.com", "WWW.Example.com", " my_app.example.com "}},
				{Name: "*.Example.com", Aliases: []string{"*.example.com."}},
			},
			names: []string{"example.com", "my_app.example.com", "www.example.com"},
			wild:  []string{"example.com"},
		},
		{
			name: "маски без родителя и регулярные выражения",
			sites: []srvinfo.Site{
				{Name: "www.example.*", Aliases: []string{"*", "*.com", "*.localhost", "a.*.example.com", "*.*.example.com"}},
				{Name: "~example.com", Aliases: []string{`~^www\d+\.example\.com$`, "(www|api).example.com"}},
			},
		},
		{
			name: "адреса и локальные имена",
			sites: []srvinfo.Site{
				{Name: "192.0.2.10", Aliases: []string{"192.0.2.010", "[2001:db8::1]", "2001:db8::1", "*.2.10"}},
				{Name: "localhost", Aliases: []string{"localhost.localdomain", "app.localhost", "intranet", "_", "$hostname"}},
				{Name: "-bad.example.com", Aliases: []string{"bad-.example.com", "a..example.com", ".", ""}},
			},
		},
		{
			name:  "папки сертификатов: повторная с хвостом, своя без точки",
			sites: []srvinfo.Site{{Name: "example.com"}},
			certs: []srvinfo.Cert{
				{Name: "example.com"}, {Name: "shop.example.com-0001"}, {Name: "mail.example.net"}, {Name: "mysite"},
				{Name: "mysite-0002"},
			},
			names: []string{"example.com", "mail.example.net", "shop.example.com"},
		},
	}
	for _, c := range cases {
		names, wild := siteNames(c.sites, c.certs)
		if !slices.Equal(names, c.names) {
			t.Errorf("%s: имена %q, жду %q", c.name, names, c.names)
		}
		if !slices.Equal(wild, c.wild) {
			t.Errorf("%s: маски %q, жду %q", c.name, wild, c.wild)
		}
	}
}
