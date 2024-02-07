package netinfo

import "testing"

func TestParseServiceOrder(t *testing.T) {
	out := "An asterisk (*) denotes that a network service is disabled.\n(1) AX88179A\n(Hardware Port: AX88179A, Device: en9)\n\n(2) Thunderbolt Bridge\n(Hardware Port: Thunderbolt Bridge, Device: bridge0)\n\n(3) Wi-Fi\n(Hardware Port: Wi-Fi, Device: en0)\n"
	list := ParseServiceOrder(out)
	if len(list) != 3 || list[2].Name != "Wi-Fi" || list[2].Device != "en0" || list[1].Device != "bridge0" {
		t.Errorf("parsed: %+v", list)
	}
}

func TestParseRouteGet(t *testing.T) {
	out := "   route to: 89.191.225.254\ndestination: 89.191.225.254\n    gateway: 10.168.20.1\n  interface: en0\n      flags: <UP,GATEWAY,HOST,DONE,STATIC>\n"
	r := ParseRouteGet(out)
	if r.Gateway != "10.168.20.1" || r.Iface != "en0" {
		t.Errorf("route: %+v", r)
	}
	if r := ParseRouteGet("   route to: 5.253.63.13\n  interface: utun4\n"); r.Gateway != "" || r.Iface != "utun4" {
		t.Errorf("tunnel route: %+v", r)
	}
}

func TestParseHostRoutes(t *testing.T) {
	out := "Routing tables\n\nInternet:\nDestination        Gateway            Flags               Netif Expire\ndefault            192.168.3.1        UGScg                 en0\n10.8.1.1           10.8.1.1           UH                  utun4\n45.128.204.161     10.168.20.1        UGHS                  en0\n192.168.3.1        8:31:a4:55:0:2e    UHLWIir               en0   1197\n"
	list := ParseHostRoutes(out)
	if len(list) != 1 || list[0].Dest != "45.128.204.161" || list[0].Gateway != "10.168.20.1" || list[0].Iface != "en0" {
		t.Errorf("host routes: %+v", list)
	}
}

func TestParseLsof(t *testing.T) {
	out := "COMMAND PID USER FD TYPE DEVICE SIZE/OFF NODE NAME\npostgres 1728 timurt 7u IPv6 0x1 0t0 TCP [::1]:5432 (LISTEN)\npostgres 1728 timurt 8u IPv4 0x2 0t0 TCP 127.0.0.1:5432 (LISTEN)\nOrbStack\\x20Helper 52572 timurt 9u IPv4 0x3 0t0 TCP *:443 (LISTEN)\n"
	list := ParseLsof(out)
	if len(list) != 2 || list[0].Port != "5432" || list[0].PID != "1728" || list[1].Port != "443" {
		t.Errorf("lsof: %+v", list)
	}
}

func TestIsPrivate(t *testing.T) {
	for ip, want := range map[string]bool{"10.16.2.5": true, "172.20.1.1": true, "172.15.0.1": false, "192.168.3.1": true, "89.191.225.254": false, "nope": false} {
		if got := IsPrivate(ip); got != want {
			t.Errorf("%s: want %v, got %v", ip, want, got)
		}
	}
}

func TestParseTarget(t *testing.T) {
	cases := []struct {
		in   string
		kind TargetKind
		val  string
	}{
		{"203.0.113.7", KindHost, "203.0.113.7"},
		{"203.0.113.77/24", KindNet, "203.0.113.0/24"},
		{"Example.COM.", KindDomain, "example.com"},
		{"https://git.example.com:8443/repo", KindDomain, "git.example.com"},
	}
	for _, c := range cases {
		got, err := ParseTarget(c.in)
		if err != nil || got.Kind != c.kind || got.Value != c.val {
			t.Errorf("%s: %+v %v", c.in, got, err)
		}
	}
	for _, bad := range []string{"300.1.1.1", "10.0.0.0/40", "localhost", "a b.c"} {
		if _, err := ParseTarget(bad); err == nil {
			t.Errorf("%q should fail", bad)
		}
	}
	priv, _ := ParseTarget("10.20.0.0/16")
	pub, _ := ParseTarget("198.51.100.0/24")
	if !PrivateTarget(priv) || PrivateTarget(pub) {
		t.Error("private detection")
	}
}

const tableSample = `Routing tables

Internet:
Destination        Gateway            Flags               Netif Expire
0/1                utun4              UScg                utun4
default            192.0.2.1          UGScg                 en0
10.8/16            10.8.0.1           UGSc                utun4
128.0/1            utun4              UScg                utun4
198.51.100/24      192.0.2.1          UGSc                  en0
198.51.100.9       192.0.2.254        UGHS                  en0
203.0.113.5        192.0.2.1          UGHS                  en0
203.0.113.6        192.168.7.1        UGHS                  en7
192.0.2.1/32       link#14            UCS                   en0      !
192.0.2.1          8:31:a4:55:0:2e    UHLWIir               en0   1197
`

func TestParseRoutesExpandsNets(t *testing.T) {
	list := ParseRoutes(tableSample)
	want := map[string]bool{"0.0.0.0/1": true, "10.8.0.0/16": true, "128.0.0.0/1": true, "198.51.100.0/24": true}
	found := 0
	for _, r := range list {
		if want[r.Dest] {
			found++
		}
	}
	if found != 4 {
		t.Errorf("nets found %d: %+v", found, list)
	}
	hosts := ParseHostRoutes(tableSample)
	if len(hosts) != 3 || hosts[0].Dest != "198.51.100.9" {
		t.Errorf("hosts %+v", hosts)
	}
}

func TestStale(t *testing.T) {
	list := ParseRoutes(tableSample)
	all := Stale(list, "192.0.2.1", nil)
	if len(all) != 2 || all[0].Dest != "198.51.100.9" || all[1].Dest != "203.0.113.6" {
		t.Errorf("stale all: %+v", all)
	}
	only := Stale(list, "192.0.2.1", map[string]bool{"203.0.113.6": true, "203.0.113.5": true})
	if len(only) != 1 || only[0].Dest != "203.0.113.6" {
		t.Errorf("stale only: %+v", only)
	}
}
