package netinfo

import (
	"strings"
	"testing"
)

func TestParseIPRoute(t *testing.T) {
	out := `default via 10.8.0.1 dev wg0 proto static
default via 192.168.1.1 dev wlp3s0 proto dhcp src 192.168.1.40 metric 600
0.0.0.0/1 dev wg0 scope link
77.88.0.0/18 via 192.168.1.1 dev wlp3s0
157.22.175.251 via 192.168.1.1 dev wlp3s0
192.168.1.0/24 dev wlp3s0 proto kernel scope link src 192.168.1.40 metric 600
`
	dev, gw := ParseIPRouteDefault(out)
	// Туннель wg0 стоит первым, но физический интерфейс - wlp3s0.
	if dev != "wlp3s0" || gw != "192.168.1.1" {
		t.Fatalf("%s %s", dev, gw)
	}
	table := ParseIPRoute(out)
	m := map[string]TableRoute{}
	for _, r := range table {
		m[r.Target()] = r
	}
	if r := m["77.88.0.0/18"]; r.Gateway != "192.168.1.1" || r.Iface != "wlp3s0" || r.Flags != "UGS" {
		t.Fatalf("%+v", r)
	}
	if r := m["157.22.175.251"]; !strings.Contains(r.Flags, "H") {
		t.Fatalf("host-маршрут: %+v", r)
	}
	if r := m["192.168.1.0/24"]; strings.Contains(r.Flags, "S") {
		t.Fatalf("маршрут ядра не статический: %+v", r)
	}
	if r := m["0.0.0.0/1"]; !IsTunnelIface(r.Iface) {
		t.Fatalf("wg0 - туннель: %+v", r)
	}
}

func TestParseIPRouteGet(t *testing.T) {
	r := ParseIPRouteGet("1.1.1.1 via 10.8.0.1 dev wg0 table 51820 src 10.8.0.2 uid 1000\n    cache\n")
	if r.Gateway != "10.8.0.1" || r.Iface != "wg0" {
		t.Fatalf("%+v", r)
	}
}
