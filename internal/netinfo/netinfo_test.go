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
