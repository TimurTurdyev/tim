package srvinfo

import "testing"

func TestSplit(t *testing.T) {
	a := Attacks{
		SSHByIP: []Count{{"103.77.192.5", 1800}, {"185.220.101.4", 412}, {"45.1.1.1", 3}},
		UFWByIP: []Count{{"45.1.1.1", 40}},
		Probes: []Probe{{IP: "5.188.10.2", Path: "/.env", File: "/var/www/a/logs/lpack.ru.access.log"},
			{IP: "5.188.10.2", Path: "/.git/config", File: "/var/www/a/logs/lpack.ru.access.log"}},
	}
	list := ByIP(a)
	for i := range list {
		switch list[i].IP {
		case "185.220.101.4":
			list[i].Lists = []string{"Tor"}
		}
	}
	targeted, noise := Split(list)
	// Прицельный - только перебор 1800 раз: Tor - в списке, 43 стука - фон, две пробы - меньше порога.
	if len(targeted) != 1 || targeted[0].IP != "103.77.192.5" {
		t.Fatalf("прицельные: %+v", targeted)
	}
	if noise.IPs != 3 || noise.Attempts != 412+43+2 || len(noise.Sources) != 1 || noise.Sources[0] != "Tor" {
		t.Fatalf("шум: %+v", noise)
	}
	if Network("185.220.101.4") != "185.220.101.0/24" || Network("2a01:4f8:1:2::5") != "2a01:4f8:1::/48" {
		t.Fatal(Network("185.220.101.4"), Network("2a01:4f8:1:2::5"))
	}
}
