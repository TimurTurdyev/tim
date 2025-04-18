package dnsq

import "testing"

func TestParseCAA(t *testing.T) {
	// 0 issue "letsencrypt.org"
	data := append([]byte{0, 5}, []byte("issueletsencrypt.org")...)
	tag, val, ok := ParseCAA(data)
	if !ok || tag != "issue" || val != "letsencrypt.org" {
		t.Fatalf("%q %q %v", tag, val, ok)
	}
	if _, _, ok := ParseCAA([]byte{0, 9, 'a'}); ok {
		t.Fatal("обрезанная запись принята")
	}
}
