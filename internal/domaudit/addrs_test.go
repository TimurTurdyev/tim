package domaudit

import "testing"

// Свой адрес - сервер самого домена: имя ведёт на него без CNAME на чужой сервис, хотя бы и в несколько шагов.
func TestDirectAddr(t *testing.T) {
	r := &collector{domain: "example.com"}
	cnames := map[string]string{
		"www.example.com":  "web.example.com",
		"web.example.com":  "",
		"shop.example.com": "lb.example.com",
		"lb.example.com":   "example.myshopify.com",
		"docs.example.com": "user.github.io",
		"loop.example.com": "pool.example.com",
		"pool.example.com": "loop.example.com",
	}
	cases := map[string]bool{
		"example.com": true, "www.example.com": true, "web.example.com": true,
		"shop.example.com": false, "docs.example.com": false, "loop.example.com": true,
	}
	for name, want := range cases {
		if got := r.direct(chain(cnames, name)); got != want {
			t.Errorf("%s: свой %v, ждали %v (%v)", name, got, want, chain(cnames, name))
		}
	}
	// Отчёт по поддомену: соседнее имя той же регистрации - тоже своё.
	if sub := (&collector{domain: "api.example.com"}); !sub.direct([]string{"lb.example.com"}) || sub.direct([]string{"lb.example.net"}) {
		t.Error("поддомен: своё и чужое перепутаны")
	}
}
