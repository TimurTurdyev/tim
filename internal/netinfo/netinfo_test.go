package netinfo

import (
	"net/netip"
	"os"
	"testing"
)

func TestParseServiceOrder(t *testing.T) {
	out := "An asterisk (*) denotes that a network service is disabled.\n(1) AX88179A\n(Hardware Port: AX88179A, Device: en9)\n\n(2) Thunderbolt Bridge\n(Hardware Port: Thunderbolt Bridge, Device: bridge0)\n\n(3) Wi-Fi\n(Hardware Port: Wi-Fi, Device: en0)\n"
	list := ParseServiceOrder(out)
	if len(list) != 3 || list[2].Name != "Wi-Fi" || list[2].Device != "en0" || list[1].Device != "bridge0" {
		t.Errorf("parsed: %+v", list)
	}
}

func TestParseRouteGet(t *testing.T) {
	out := "   route to: 203.0.113.20\ndestination: 203.0.113.20\n    gateway: 10.168.20.1\n  interface: en0\n      flags: <UP,GATEWAY,HOST,DONE,STATIC>\n"
	r := ParseRouteGet(out)
	if r.Gateway != "10.168.20.1" || r.Iface != "en0" {
		t.Errorf("route: %+v", r)
	}
	if r := ParseRouteGet("   route to: 203.0.113.30\n  interface: utun4\n"); r.Gateway != "" || r.Iface != "utun4" {
		t.Errorf("tunnel route: %+v", r)
	}
}

func TestParseLsof(t *testing.T) {
	out := "COMMAND PID USER FD TYPE DEVICE SIZE/OFF NODE NAME\npostgres 1728 dev 7u IPv6 0x1 0t0 TCP [::1]:5432 (LISTEN)\npostgres 1728 dev 8u IPv4 0x2 0t0 TCP 127.0.0.1:5432 (LISTEN)\nOrbStack\\x20Helper 52572 dev 9u IPv4 0x3 0t0 TCP *:443 (LISTEN)\n"
	list := ParseLsof(out)
	if len(list) != 2 || list[0].Port != "5432" || list[0].PID != "1728" || list[1].Port != "443" {
		t.Errorf("lsof: %+v", list)
	}
}

func TestParseRoutesExpandsNets(t *testing.T) {
	list := ParseRoutes(sample(t, "netstat-rn.txt"))
	want := map[string]bool{"0.0.0.0/1": true, "10.8.0.0/16": true, "128.0.0.0/1": true, "198.51.100.0/24": true}
	found := 0
	for _, r := range list {
		if want[r.Target()] {
			found++
		}
	}
	if found != 4 {
		t.Errorf("nets found %d: %+v", found, list)
	}
}

func TestStale(t *testing.T) {
	list := ParseRoutes(sample(t, "netstat-rn.txt"))
	all := Stale(list, "192.0.2.1", nil)
	if len(all) != 2 || all[0].Target() != "198.51.100.9" || all[1].Target() != "203.0.113.6" {
		t.Errorf("stale all: %+v", all)
	}
	only := Stale(list, "192.0.2.1", map[netip.Prefix]bool{netip.MustParsePrefix("203.0.113.6/32"): true, netip.MustParsePrefix("203.0.113.5/32"): true})
	if len(only) != 1 || only[0].Target() != "203.0.113.6" {
		t.Errorf("stale only: %+v", only)
	}
}

func TestParseSS(t *testing.T) {
	out := `LISTEN 0      4096         0.0.0.0:22        0.0.0.0:*    users:(("sshd",pid=812,fd=3))
LISTEN 0      4096            [::]:22           [::]:*    users:(("sshd",pid=812,fd=4))
LISTEN 0      511        127.0.0.1:5432      0.0.0.0:*
LISTEN 0      128   [::ffff:127.0.0.1]:8000         *:*    users:(("php",pid=99,fd=7))
`
	list := ParseSS(out)
	if len(list) != 3 || list[0].Port != "22" || list[0].PID != "812" || list[0].Command != "sshd" || list[1].Port != "5432" || list[1].PID != "" || list[2].Port != "8000" {
		t.Errorf("ss: %+v", list)
	}
}

func TestLookupNarrowest(t *testing.T) {
	table := ParseRoutes(`Destination        Gateway            Flags               Netif Expire
0/1                utun4              USc                 utun4
10.88/18           192.0.2.1          UGSc                  en0
10.88.8.8          192.0.2.1          UGHSI                 en0
`)
	if r, ok := Lookup(table, "10.88.8.8"); !ok || r.Target() != "10.88.0.0/18" {
		t.Errorf("сеть уже маршрута на адрес с флагом I: %+v", r)
	}
	if r, ok := Lookup(table, "10.5.5.5"); !ok || r.Iface != "utun4" {
		t.Errorf("0/1 туннеля: %+v", r)
	}
	if _, ok := Lookup(table, "203.0.113.99"); ok {
		t.Error("для 203.0.113.99 в таблице только маршрут по умолчанию - его тут нет")
	}
}

// sample: образец вывода утилиты из testdata.
func sample(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
