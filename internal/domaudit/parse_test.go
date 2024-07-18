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

func TestTitleAndTrackers(t *testing.T) {
	body := `<html><head><TITLE>  Мой   магазин &amp; сервис
	</TITLE><script>ym(12345678, "init", {});</script>
	<script src="https://www.googletagmanager.com/gtag/js?id=G-AB12CD34"></script>
	<script>gtag('config','UA-1234567-2');</script><!-- GTM-ABC1234 -->
	<ins class="adsbygoogle" data-ad-client="ca-pub-1234567890123456"></ins></head></html>`
	if got := Title(body); got != "Мой магазин & сервис" {
		t.Errorf("title %q", got)
	}
	if Title("<html><body>нет заголовка</body></html>") != "" {
		t.Error("missing title")
	}
	tr := Trackers(body)
	kinds := map[string]string{}
	for _, x := range tr {
		kinds[x.Kind] = x.ID
	}
	if kinds["Яндекс.Метрика"] != "12345678" || kinds["Google Tag Manager"] != "GTM-ABC1234" || kinds["AdSense"] != "ca-pub-1234567890123456" {
		t.Errorf("trackers %+v", tr)
	}
	if len(tr) != 5 {
		t.Errorf("want 5 trackers (Метрика, два GA, GTM, AdSense), got %d: %+v", len(tr), tr)
	}
}

func TestParseNeighbors(t *testing.T) {
	out := "example.com\nshop.example.com\n\nvery bad line\nEXAMPLE.ORG\nexample.com\n-bad-.com\n"
	got := ParseNeighbors(out, "example.com")
	if len(got) != 2 || got[0] != "example.org" || got[1] != "shop.example.com" {
		t.Errorf("%v", got)
	}
}
