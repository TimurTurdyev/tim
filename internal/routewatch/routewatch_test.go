package routewatch

import (
	"testing"

	"github.com/TimurTurdyev/tim/internal/netinfo"
)

func TestDecide(t *testing.T) {
	table := netinfo.ParseRoutes(`Destination        Gateway            Flags               Netif Expire
default            10.168.20.1        UGScg                 en0
77.88/18           192.168.3.1        UGSc                  en0
185.73.192/22      10.168.20.1        UGSc                  en0
5.255.192/18       10.8.1.1           UGSc                  utun4
157.22.175.251     10.168.20.1        UGHS                  en0
`)
	want := []string{"77.88.0.0/18", "185.73.192.0/22", "5.255.192.0/18", "157.22.175.251", "141.8.128.0/18", "185.73.192.0/22"}
	got := Decide(want, table, "10.168.20.1", "en0")
	kinds := map[string]Kind{}
	for _, r := range got {
		kinds[r.Dest] = r.Kind
	}
	// Интерфейс en0 правильный, а шлюз от прошлой сети: по интерфейсу поломку не видно.
	if kinds["77.88.0.0/18"] != Gateway {
		t.Errorf("старый шлюз: %v", kinds)
	}
	if kinds["5.255.192.0/18"] != Tunnel || kinds["141.8.128.0/18"] != Missing {
		t.Errorf("%v", kinds)
	}
	if _, bad := kinds["185.73.192.0/22"]; bad || len(got) != 3 {
		t.Errorf("живые маршруты трогать нельзя, повторы считать один раз: %v", got)
	}
	if _, bad := kinds["157.22.175.251"]; bad {
		t.Error("host-маршрут через текущий шлюз исправен")
	}
}
