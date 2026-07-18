package netinfo

import "testing"

func TestParseTarget(t *testing.T) {
	cases := []struct {
		in   string
		kind TargetKind
		val  string
	}{
		{"203.0.113.7", KindHost, "203.0.113.7"},
		{"203.0.113.7:22", KindHost, "203.0.113.7"},
		{"10.0.0.5:22", KindHost, "10.0.0.5"},
		{"203.0.113.7/32", KindHost, "203.0.113.7"},
		{"203.0.113.77/24", KindNet, "203.0.113.0/24"},
		{"as:64500", KindAS, "as:64500"},
		{"AS64500", KindAS, "as:64500"},
		{"as064500", KindAS, "as:64500"},
		{"Example.COM.", KindDomain, "example.com"},
		{"Example.COM/path", KindDomain, "example.com"},
		{"https://git.example.com:8443/repo", KindDomain, "git.example.com"},
		{"пример.рф", KindDomain, "xn--e1afmkfd.xn--p1ai"},
		// Домены на "as" не должны превращаться в номер.
		{"asana.com", KindDomain, "asana.com"},
	}
	for _, c := range cases {
		got, err := ParseTarget(c.in)
		if err != nil || got.Kind != c.kind || got.Value != c.val {
			t.Errorf("%s: %+v %v", c.in, got, err)
		}
	}
	for _, bad := range []string{"300.1.1.1", "10.0.0.0/40", "localhost", "a b.c", ""} {
		if _, err := ParseTarget(bad); err == nil {
			t.Errorf("%q should fail", bad)
		}
	}
}
