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

func TestParseCymru(t *testing.T) {
	out := "Bulk mode; whois.cymru.com [2025-10-18 20:17:46 +0000]\n" +
		"15169   | 8.8.8.8          | 8.8.8.0/24          | US | arin     | 2023-12-28 | GOOGLE - Google LLC, US\n" +
		"NA      | 10.0.0.1         | NA                  | NA | other    | NA         | NA\n" +
		"13335   | 1.1.1.1          | 1.1.1.0/24          | AU | apnic    | 2011-08-11 | CLOUDFLARENET - Cloudflare, Inc., US\n"
	m := ParseCymru(out)
	if len(m) != 2 || m["8.8.8.8"].ASN != "15169" || m["8.8.8.8"].Country != "US" {
		t.Fatalf("%+v", m)
	}
	if !IsCDN(m["1.1.1.1"].Name) || IsCDN("TIMEWEB-AS, RU") {
		t.Error("cdn detection")
	}
}

func TestParseHostSearch(t *testing.T) {
	subs, err := ParseHostSearch("admin.example.com,1.2.3.4\nwww.example.com,1.2.3.4\nother.org,5.6.7.8\n", "example.com")
	if err != nil || len(subs) != 2 || subs[0] != "admin.example.com" {
		t.Fatalf("%v %v", subs, err)
	}
	if _, err := ParseHostSearch("error API count exceeded\n", "example.com"); err == nil {
		t.Error("quota error should be reported")
	}
}
