package hostsfile

import (
	"strings"
	"testing"
)

const original = `##
# Host Database
##
127.0.0.1	localhost
255.255.255.255	broadcasthost
10.0.0.5 nas.home
`

func TestRoundTrip(t *testing.T) {
	var e []Entry
	e = Add(e, "127.0.0.1", []string{"shop.test", "api.shop.test"}, "dev")
	e = Add(e, "127.0.0.1", []string{"old.test"}, "legacy")
	text := Render(original, e)
	if !strings.Contains(text, "10.0.0.5 nas.home") || !strings.Contains(text, "broadcasthost") {
		t.Fatal("чужие записи потерялись")
	}
	back := Parse(text)
	if len(back) != 2 {
		t.Fatalf("прочитано %d записей:\n%s", len(back), text)
	}
	if back[0].Profile != "dev" || strings.Join(back[0].Names, ",") != "api.shop.test,shop.test" &&
		strings.Join(back[0].Names, ",") != "shop.test,api.shop.test" {
		t.Fatalf("первая запись: %+v", back[0])
	}
	// Повторная запись того же набора не плодит блоки.
	again := Render(text, back)
	if strings.Count(again, Begin) != 1 || strings.Count(again, End) != 1 {
		t.Fatalf("блоков стало больше одного:\n%s", again)
	}
	if Render(again, nil) != strings.TrimRight(original, "\n")+"\n" {
		t.Fatalf("удаление блока не вернуло файл к исходному:\n%q", Render(again, nil))
	}
}

func TestSwitchAndParseOff(t *testing.T) {
	var e []Entry
	e = Add(e, "127.0.0.1", []string{"a.test"}, "dev")
	e = Add(e, "127.0.0.1", []string{"b.test"}, "dev")
	e = Add(e, "127.0.0.1", []string{"c.test"}, "other")
	// a.test и b.test с одним адресом и профилем сливаются в одну строку.
	if len(e) != 2 {
		t.Fatalf("записей %d, ожидали 2: %+v", len(e), e)
	}
	e, n := Switch(e, "dev", false)
	if n != 1 {
		t.Fatalf("выключено %d записей, ожидали 1", n)
	}
	if _, n := Switch(e, "dev", false); n != 0 {
		t.Fatal("повторное выключение ничего менять не должно")
	}
	back := Parse(Render(original, e))
	on := 0
	for _, x := range back {
		if x.On {
			on++
		}
	}
	if on != 1 {
		t.Fatalf("включённых записей %d, ожидали 1: %+v", on, back)
	}
	names, counts := Profiles(back)
	if strings.Join(names, ",") != "dev,other" {
		t.Fatalf("профили: %v", names)
	}
	if counts["dev"] != [2]int{0, 1} || counts["other"] != [2]int{1, 1} {
		t.Fatalf("счёт: %v", counts)
	}
}
