package inspect

import (
	"strings"
	"testing"
)

func levels(fs []Finding) string {
	var out []string
	for _, f := range fs {
		word, _ := f.Level.MarshalText()
		out = append(out, string(word))
	}
	return strings.Join(out, ",")
}

func TestAnalyzeSPF(t *testing.T) {
	cases := []struct {
		txt  []string
		want string
	}{
		{nil, "bad"},
		{[]string{"google-site-verification=x", "v=spf1 include:_spf.google.com -all"}, "ok"},
		{[]string{"v=spf1 mx ~all"}, "ok"},
		{[]string{"v=spf1 +all"}, "bad"},
		{[]string{"v=spf1 a mx"}, "warn"},
		{[]string{"v=spf1 redirect=_spf.example.com"}, ""},
		{[]string{"v=spf1 -all", "v=spf1 ~all"}, "bad"},
		{[]string{"v=spf1 include:a include:b include:c include:d include:e include:f a mx include:g include:h include:i ~all"}, "ok,warn"},
	}
	for _, c := range cases {
		if _, fs := AnalyzeSPF(c.txt); levels(fs) != c.want {
			t.Errorf("%v: %s", c.txt, levels(fs))
		}
	}
}

func TestAnalyzeDMARC(t *testing.T) {
	cases := []struct {
		txt  []string
		want string
	}{
		{nil, "bad"},
		{[]string{"v=DMARC1; p=reject; rua=mailto:d@example.com"}, "ok"},
		{[]string{"v=DMARC1; p=none"}, "warn,info"},
		{[]string{"v=DMARC1; p=quarantine; pct=25; rua=mailto:x@y"}, "ok,warn"},
		{[]string{"v=DMARC1; rua=mailto:x@y"}, "bad"},
	}
	for _, c := range cases {
		if _, fs := AnalyzeDMARC(c.txt); levels(fs) != c.want {
			t.Errorf("%v: %s", c.txt, levels(fs))
		}
	}
}
