package hook

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandlerJournalReplay(t *testing.T) {
	j := &Journal{Path: t.TempDir() + "/hooks.jsonl"}
	got := make(chan Record, 1)
	srv := httptest.NewServer(Handler(202, []byte("ok"), func(r Record) {
		rec, err := j.Add(r)
		if err != nil {
			t.Error(err)
		}
		got <- rec
	}))
	defer srv.Close()
	req, _ := http.NewRequest("POST", srv.URL+"/pay/callback?shop=1", strings.NewReader(`{"event":"payment.succeeded","amount":{"value":"100.00"}}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Signature", "abc")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != 202 {
		t.Fatalf("статус %d", resp.StatusCode)
	}
	rec := <-got
	if rec.N != 1 || rec.Path != "/pay/callback?shop=1" {
		t.Fatalf("%+v", rec)
	}
	p := Pretty(rec)
	if !strings.Contains(p, "X-Signature: abc") || !strings.Contains(p, `"event": "payment.succeeded"`) {
		t.Fatalf("разбор:\n%s", p)
	}

	// Повтор в свой сервер приходит с тем же телом, путём и подписью.
	var seen *http.Request
	var seenBody string
	dev := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r
		b, _ := io.ReadAll(r.Body)
		seenBody = string(b)
	}))
	defer dev.Close()
	again, ok := j.Find(1)
	if !ok {
		t.Fatal("в журнале нет запроса 1")
	}
	resp, err = Replay(again, dev.URL, http.DefaultClient)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if seen.URL.RequestURI() != "/pay/callback?shop=1" || seen.Header.Get("X-Signature") != "abc" || !strings.Contains(seenBody, "100.00") {
		t.Fatalf("повтор: %s %v %q", seen.URL, seen.Header, seenBody)
	}
}

func TestJournalKeep(t *testing.T) {
	j := &Journal{Path: t.TempDir() + "/h.jsonl"}
	for i := 0; i < Keep+5; i++ {
		if _, err := j.Add(Record{Method: "GET"}); err != nil {
			t.Fatal(err)
		}
	}
	all := j.Load()
	if len(all) != Keep || all[len(all)-1].N != Keep+5 {
		t.Fatalf("в журнале %d, последний %d", len(all), all[len(all)-1].N)
	}
}

func TestPrettyForm(t *testing.T) {
	r := Record{Headers: http.Header{"Content-Type": {"application/x-www-form-urlencoded"}}, Body: []byte("b=2&a=1")}
	if p := Pretty(r); !strings.Contains(p, "a = 1\n  b = 2") {
		t.Fatalf("%s", p)
	}
}
