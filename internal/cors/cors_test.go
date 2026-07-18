package cors

import (
	"net/http"
	"strings"
	"testing"
)

func h(kv ...string) http.Header {
	out := http.Header{}
	for i := 0; i+1 < len(kv); i += 2 {
		out.Add(kv[i], kv[i+1])
	}
	return out
}

func texts(ps []Problem) string {
	var s []string
	for _, p := range ps {
		s = append(s, p.Text)
	}
	return strings.Join(s, "|")
}

func TestPreflightMissingHeader(t *testing.T) {
	r := Request{Origin: "https://app.example.com", Method: "PUT", Headers: []string{"Authorization", "Content-Type"}}
	if !NeedsPreflight(r) {
		t.Fatal("PUT с Authorization требует preflight")
	}
	pre := h("Access-Control-Allow-Origin", "*", "Access-Control-Allow-Methods", "GET, PUT", "Access-Control-Allow-Headers", "*")
	got := Analyze(r, 204, pre, h("Access-Control-Allow-Origin", "*"))
	// Звёздочка в Allow-Headers не покрывает Authorization - классическая ловушка.
	if len(got) != 1 || !strings.Contains(got[0].Text, "Authorization") {
		t.Fatalf("%s", texts(got))
	}
}

func TestCredentialsWithStar(t *testing.T) {
	r := Request{Origin: "https://app.example.com", Method: "GET", Credentials: true}
	got := Analyze(r, 0, nil, h("Access-Control-Allow-Origin", "*"))
	if len(got) != 1 || !got[0].Bad {
		t.Fatalf("%s", texts(got))
	}
}
