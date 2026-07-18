package netinfo

import (
	"strings"
	"testing"
)

const ipRouteSample = `default via 10.8.0.1 dev wg0 proto static
default via 192.168.1.1 dev wlp3s0 proto dhcp src 192.168.1.40 metric 600
0.0.0.0/1 dev wg0 scope link
10.88.0.0/18 via 192.168.1.1 dev wlp3s0
203.0.113.40 via 192.168.1.1 dev wlp3s0
192.168.1.0/24 dev wlp3s0 proto kernel scope link src 192.168.1.40 metric 600
`

func TestParseIPRouteDefault(t *testing.T) {
	dev, gw := ParseIPRouteDefault(ipRouteSample)
	// Туннель wg0 стоит первым, но физический интерфейс - wlp3s0.
	if dev != "wlp3s0" || gw != "192.168.1.1" {
		t.Fatalf("%s %s", dev, gw)
	}
}

func TestParseIPRoute(t *testing.T) {
	table := ParseIPRoute(ipRouteSample)
	m := map[string]TableRoute{}
	for _, r := range table {
		m[r.Target()] = r
	}
	if r := m["10.88.0.0/18"]; r.Gateway != "192.168.1.1" || r.Iface != "wlp3s0" || r.Flags != "UGS" {
		t.Fatalf("%+v", r)
	}
	if r := m["203.0.113.40"]; !strings.Contains(r.Flags, "H") {
		t.Fatalf("host-маршрут: %+v", r)
	}
	if r := m["192.168.1.0/24"]; strings.Contains(r.Flags, "S") {
		t.Fatalf("маршрут ядра не статический: %+v", r)
	}
	if r := m["0.0.0.0/1"]; r.Iface != "wg0" {
		t.Fatalf("wg0 - туннель: %+v", r)
	}
}

func TestParseIPRouteGet(t *testing.T) {
	r := ParseIPRouteGet("1.1.1.1 via 10.8.0.1 dev wg0 table 51820 src 10.8.0.2 uid 1000\n    cache\n")
	if r.Gateway != "10.8.0.1" || r.Iface != "wg0" {
		t.Fatalf("%+v", r)
	}
}
