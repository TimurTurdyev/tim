package vpnhelp

import "testing"

func TestParseList(t *testing.T) {
	got := ParseList("# CIDR или домен\n31.129.46.0/23   # gosport.shop\n\nmarket.example.ru\n  # просто комментарий\n")
	if len(got) != 2 || got[0].Addr != "31.129.46.0/23" || got[0].Label != "gosport.shop" || got[1].Addr != "market.example.ru" {
		t.Fatalf("%+v", got)
	}
}

func TestParseScript(t *testing.T) {
	script := `#!/bin/bash
LABEL=x
NETS=(
  # Ozon
  185.73.192.0/22
  # Яндекс AS13238
  87.250.224.0/19
  77.88.0.0/18   # морда
)
apply() { echo "NETS=( fake )"; }
`
	got := ParseScript(script)
	if len(got) != 3 {
		t.Fatalf("%+v", got)
	}
	if got[0].Label != "Ozon" || got[1].Label != "Яндекс AS13238" || got[2].Label != "морда" {
		t.Fatalf("%+v", got)
	}
	// Однострочный массив тоже бывает.
	if one := ParseScript(`NETS=(10.0.0.0/8 "192.0.2.0/24")`); len(one) != 2 || one[1].Addr != "192.0.2.0/24" {
		t.Fatalf("%+v", one)
	}
}
