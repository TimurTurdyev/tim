package inspect

import (
	"strings"
	"testing"
)

func levels(fs []Finding) string {
	var out []string
	for _, f := range fs {
		out = append(out, f.Level)
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

func TestVerdict(t *testing.T) {
	build := func(spf, dmarc string, mx []string, null bool, dkim bool) *Mail {
		m := &Mail{MX: mx, NullMX: null, DKIM: map[string]string{}}
		if dkim {
			m.DKIM["mail"] = "v=DKIM1; p=abc"
		}
		m.SPF, m.SPFF = AnalyzeSPF([]string{spf})
		m.DMARC, m.DMRF = AnalyzeDMARC([]string{dmarc})
		return m
	}
	cases := []struct {
		name    string
		m       *Mail
		level   string
		reasons int
	}{
		{"всё есть", build("v=spf1 -all", "v=DMARC1; p=reject; rua=mailto:a@b", []string{"mx 10"}, false, true), "ok", 0},
		{"нет DMARC", build("v=spf1 -all", "", []string{"mx 10"}, false, true), "bad", 1},
		{"DMARC только наблюдает", build("v=spf1 -all", "v=DMARC1; p=none; rua=mailto:a@b", []string{"mx 10"}, false, true), "warn", 1},
		{"нет DKIM", build("v=spf1 -all", "v=DMARC1; p=reject; rua=mailto:a@b", []string{"mx 10"}, false, false), "warn", 1},
		{"нет MX", build("v=spf1 -all", "v=DMARC1; p=reject; rua=mailto:a@b", nil, false, true), "bad", 1},
		{"null MX с записями", build("v=spf1 -all", "v=DMARC1; p=reject; rua=mailto:a@b", nil, true, true), "ok", 0},
		{"SPF всем разрешает", build("v=spf1 +all", "v=DMARC1; p=reject; rua=mailto:a@b", []string{"mx 10"}, false, true), "bad", 1},
	}
	for _, c := range cases {
		level, reasons := c.m.Verdict()
		if level != c.level || len(reasons) != c.reasons {
			t.Errorf("%s: %s %v, want %s c %d причинами", c.name, level, reasons, c.level, c.reasons)
		}
		for _, r := range reasons {
			if r == "" {
				t.Errorf("%s: пустая причина в %v", c.name, reasons)
			}
		}
	}
}
