package asn

import (
	"strings"
	"testing"
)

func TestAggregate(t *testing.T) {
	got := Aggregate([]string{
		"77.88.0.0/19", "77.88.32.0/19", // соседние половины -> 77.88.0.0/18
		"77.88.5.0/24",                     // внутри уже покрытой сети
		"5.255.192.0/19", "5.255.224.0/19", // ещё пара -> 5.255.192.0/18
		"87.250.250.0/24",
		"2a02:6b8::/32", // IPv6 не берём
		"мусор",
	})
	want := "5.255.192.0/18 77.88.0.0/18 87.250.250.0/24"
	if strings.Join(got, " ") != want {
		t.Fatalf("got %v", got)
	}
}

func TestAggregateNotSiblings(t *testing.T) {
	// 10.0.1.0/24 и 10.0.2.0/24 рядом, но не половины одной /23 - склеивать нельзя.
	got := Aggregate([]string{"10.0.1.0/24", "10.0.2.0/24"})
	if len(got) != 2 {
		t.Fatalf("got %v", got)
	}
	// Склейка каскадом: четыре /26 -> одна /24.
	got = Aggregate([]string{"10.0.0.0/26", "10.0.0.64/26", "10.0.0.128/26", "10.0.0.192/26"})
	if strings.Join(got, " ") != "10.0.0.0/24" {
		t.Fatalf("каскад: %v", got)
	}
}

func TestParseRIPEstat(t *testing.T) {
	data := []byte(`{"status":"ok","data":{"prefixes":[{"prefix":"77.88.0.0/18"},{"prefix":"2a02:6b8::/29"},{"prefix":"185.73.193.7/22"}]}}`)
	got, err := ParseRIPEstat(data)
	if err != nil || strings.Join(got, " ") != "77.88.0.0/18 185.73.192.0/22" {
		t.Fatalf("%v %v", got, err)
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
