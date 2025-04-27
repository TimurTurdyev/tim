package routewatch

import (
	"testing"
	"time"

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
	reasons := map[string]string{}
	for _, r := range got {
		reasons[r.Dest] = r.Reason
	}
	// Тот самый случай 2025-10-21: интерфейс en0 правильный, а шлюз от прошлой сети.
	if reasons["77.88.0.0/18"] != "шлюз 192.168.3.1 вместо 10.168.20.1" {
		t.Errorf("старый шлюз: %q", reasons["77.88.0.0/18"])
	}
	if reasons["5.255.192.0/18"] != "ушёл в туннель utun4" || reasons["141.8.128.0/18"] != "нет маршрута" {
		t.Errorf("%v", reasons)
	}
	if _, bad := reasons["185.73.192.0/22"]; bad || len(got) != 3 {
		t.Errorf("живые маршруты трогать нельзя, повторы считать один раз: %v", got)
	}
	if _, bad := reasons["157.22.175.251"]; bad {
		t.Error("host-маршрут через текущий шлюз исправен")
	}
}

func TestStatusRepairsAndSummary(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	s := Status{PID: 1, Started: now}
	s.AddRepairs([]Repair{{Dest: "a", Reason: "шлюз 1 вместо 2", At: now.Add(-48 * time.Hour)}, {Dest: "b", Reason: "ушёл в туннель utun4", At: now}})
	if err := s.Save(dir); err != nil {
		t.Fatal(err)
	}
	got, ok := Load(dir)
	if !ok || got.Since(now.Add(-24*time.Hour)) != 1 {
		t.Fatalf("%+v", got)
	}
	sum := Summary(got.Repairs)
	if sum["сменилась сеть"] != 1 || sum["VPN забрал маршрут"] != 1 {
		t.Fatalf("%v", sum)
	}
	for i := 0; i < KeepRepairs+10; i++ {
		s.AddRepairs([]Repair{{Dest: "x"}})
	}
	if len(s.Repairs) != KeepRepairs {
		t.Fatalf("журнал не обрезается: %d", len(s.Repairs))
	}
}
