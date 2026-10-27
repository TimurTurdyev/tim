package baseline

import (
	"testing"
	"time"
)

func items(kind string, keys ...string) []Item {
	var out []Item
	for _, k := range keys {
		out = append(out, Item{Kind: kind, Key: k})
	}
	return out
}

func TestObserve(t *testing.T) {
	t.Setenv("TIM_STATE_DIR", t.TempDir())
	t1 := time.Date(2026, 10, 27, 18, 0, 0, 0, time.UTC)
	t2, t3, t4 := t1.Add(15*time.Hour), t1.Add(20*time.Hour), t1.Add(30*time.Hour)

	r := Observe("srv", Snapshot{"key": items("key", "a", "b"), "port": items("port", "22")}, t1)
	if len(r.New) != 0 || r.First["key"] != 2 || r.First["port"] != 1 {
		t.Fatalf("первое наблюдение: %+v", r)
	}
	// Ключи не прочитаны (нет вида в снимке) - они не трогаются; новый порт - новое.
	r = Observe("srv", Snapshot{"port": items("port", "22", "6379")}, t2)
	if len(r.New) != 1 || r.New[0].Key != "6379" || !r.New[0].After.Equal(t1) || !r.New[0].Seen.Equal(t2) {
		t.Fatalf("новый порт: %+v", r.New)
	}
	// Новое держится и на следующем обходе, с прежним временем.
	r = Observe("srv", Snapshot{"port": items("port", "22", "6379"), "key": items("key", "a", "b", "c")}, t3)
	if len(r.New) != 2 || !r.New[0].Seen.Equal(t2) || r.New[1].Key != "c" {
		t.Fatalf("новое держится: %+v", r.New)
	}
	if err := Accept("srv", "key", "c"); err != nil {
		t.Fatal(err)
	}
	// Отмеченное своим не возвращается; пропавший порт уходит из нового, закрытый своё не забывает.
	r = Observe("srv", Snapshot{"port": items("port"), "key": items("key", "a", "b", "c")}, t4)
	if len(r.New) != 0 {
		t.Fatalf("после отметки и пропажи: %+v", r.New)
	}
	r = Observe("srv", Snapshot{"port": items("port", "22")}, t4.Add(time.Hour))
	if len(r.New) != 0 {
		t.Fatalf("снова открытый свой порт: %+v", r.New)
	}
}

func TestPrev(t *testing.T) {
	t.Setenv("TIM_STATE_DIR", t.TempDir())
	now := time.Now()
	Observe("dom", Snapshot{"MX": items("MX", "mx.yandex.net")}, now)
	r := Observe("dom", Snapshot{"MX": items("MX", "mx.relay.example")}, now.Add(time.Hour))
	if len(r.New) != 1 || r.New[0].Prev != "mx.yandex.net" {
		t.Fatalf("было: %+v", r.New)
	}
}
