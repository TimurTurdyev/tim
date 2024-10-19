package inspect

import "testing"

func TestRegistrable(t *testing.T) {
	cases := map[string]string{
		"shop.example.co.uk": "example.co.uk",
		"www.example.com.":   "example.com",
		"example.ru":         "example.ru",
		"a.b.site.рф":        "site.рф",
		"localhost":          "localhost",
	}
	for in, want := range cases {
		if got := Registrable(in); got != want {
			t.Errorf("%s: %s want %s", in, got, want)
		}
	}
}
