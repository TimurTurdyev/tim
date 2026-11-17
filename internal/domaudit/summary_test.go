package domaudit

import (
	"reflect"
	"testing"

	"github.com/TimurTurdyev/tim/internal/asn"
)

func TestNetworks(t *testing.T) {
	amazon := func(prefix string) asn.ASInfo {
		return asn.ASInfo{ASN: "64500", Prefix: prefix, Country: "US", Name: "BIGCLOUD - Big Cloud, Inc., US"}
	}
	rep := &Report{Addrs: []Addr{
		{IP: "192.0.2.1", AS: amazon("192.0.2.0/24"), Country: "IE", Hosts: []string{"a.example.com", "b.example.com"}},
		{IP: "192.0.2.2", AS: amazon("192.0.2.0/24"), Country: "DE", Hosts: []string{"b.example.com"}, MX: []string{"mx.example.com"}},
		{IP: "198.51.100.1", AS: amazon("198.51.100.0/24"), CDN: true, Hosts: []string{"c.example.com"}},
		{IP: "203.0.113.1", AS: asn.ASInfo{ASN: "64501", Prefix: "203.0.113.0/24", Name: "HOST"}, Country: "DE", Hosts: []string{}, NS: []string{"ns1.example.net"}},
		{IP: "10.0.0.1", Internal: true, Hosts: []string{"vpn.example.com"}},
	}}
	want := []Network{
		{ASN: "64500", Owner: "Big Cloud, Inc.", Prefixes: []string{"192.0.2.0/24"}, Countries: []string{"DE", "IE"}, MX: true, Addrs: 2, Hosts: 2},
		{Internal: true, Addrs: 1, Hosts: 1},
		{ASN: "64500", Owner: "Big Cloud, Inc.", Prefixes: []string{"198.51.100.0/24"}, CDN: true, Addrs: 1, Hosts: 1},
		{ASN: "64501", Owner: "HOST", Prefixes: []string{"203.0.113.0/24"}, Countries: []string{"DE"}, NS: true, Addrs: 1},
	}
	if got := Networks(rep); !reflect.DeepEqual(got, want) {
		t.Errorf("сети:\n%+v\nждали:\n%+v", got, want)
	}
	countries := []Country{{Code: "IE", Addrs: 1, Hosts: 2}, {Code: "DE", Addrs: 2, Hosts: 1}, {CDN: true, Addrs: 1, Hosts: 1}}
	if got := Countries(rep); !reflect.DeepEqual(got, countries) {
		t.Errorf("страны:\n%+v\nждали:\n%+v", got, countries)
	}
}
