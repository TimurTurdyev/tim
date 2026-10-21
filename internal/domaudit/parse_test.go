package domaudit

import "testing"

const crtSample = `[
 {"common_name":"example.com","name_value":"*.example.com\nexample.com"},
 {"common_name":"api.example.com","name_value":"api.example.com\nold.api.example.com"},
 {"common_name":"shop.example.net","name_value":"shop.example.net\nexample.net\nwww.example.co.uk"},
 {"common_name":"example.com","name_value":"api.example.com"}
]`

func TestParseCRT(t *testing.T) {
	subs, related, err := ParseCRT([]byte(crtSample), "example.com")
	if err != nil {
		t.Fatal(err)
	}
	wantSubs := []string{"api.example.com", "example.com", "old.api.example.com"}
	if len(subs) != len(wantSubs) {
		t.Fatalf("subs %v", subs)
	}
	for i := range wantSubs {
		if subs[i] != wantSubs[i] {
			t.Fatalf("subs %v want %v", subs, wantSubs)
		}
	}
	wantRel := []string{"example.co.uk", "example.net"}
	if len(related) != 2 || related[0] != wantRel[0] || related[1] != wantRel[1] {
		t.Errorf("related %v want %v", related, wantRel)
	}
	if _, _, err := ParseCRT([]byte("not json"), "example.com"); err == nil {
		t.Error("broken json should fail")
	}
}

func TestParseHostSearch(t *testing.T) {
	subs, err := ParseHostSearch("admin.example.com,192.0.2.4\nwww.example.com,192.0.2.4\nother.org,192.0.2.8\n", "example.com")
	if err != nil || len(subs) != 2 || subs[0] != "admin.example.com" {
		t.Fatalf("%v %v", subs, err)
	}
	if _, err := ParseHostSearch("error API count exceeded\n", "example.com"); err == nil {
		t.Error("quota error should be reported")
	}
}

func TestMatchTakeover(t *testing.T) {
	cases := []struct {
		target   string
		resolves bool
		body     string
		want     string // сервис; пусто - захвата нет
	}{
		{"user.github.io", true, "<h1>There isn't a GitHub Pages site here.</h1>", "GitHub Pages"},
		{"user.github.io", true, "<h1>My project</h1>", ""},
		{"app-1234.herokuapp.com", false, "", "Heroku"},
		{"shop.azurewebsites.net", false, "", "Azure"},
		{"shop.azurewebsites.net", true, "<html>ok</html>", ""},
		{"cdn.example-hosting.com", false, "", ""},
		{"", false, "", ""},
	}
	for _, c := range cases {
		got, ok := MatchTakeover(c.target, c.resolves, c.body)
		if ok != (c.want != "") || got.Service != c.want {
			t.Errorf("%s resolves=%v: %+v %v, ждали %q", c.target, c.resolves, got, ok, c.want)
		}
	}
}
