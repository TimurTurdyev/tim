package domaudit

import "testing"

// При wildcard резолвится любое имя: остаются те, что ведут не туда же, и те, на что выпускали сертификат.
func TestBlank(t *testing.T) {
	wild := []string{"192.0.2.1", "192.0.2.2", "2001:db8::1"}
	cases := []struct {
		source string
		ips    []string
		wild   []string
		want   bool
	}{
		{"THC", []string{"192.0.2.2", "192.0.2.1"}, wild, true},
		{"THC", []string{"192.0.2.1"}, wild, true},
		{fromBrute, []string{"192.0.2.1", "2001:db8::1"}, wild, true},
		{"THC", []string{"192.0.2.1", "198.51.100.7"}, wild, false},
		{"hackertarget", []string{"198.51.100.7"}, wild, false},
		{fromCert, []string{"192.0.2.1"}, wild, false},
		{fromApex, []string{"192.0.2.1"}, wild, false},
		{"THC", nil, wild, false},
		{"THC", []string{"192.0.2.1"}, nil, false},
	}
	for _, c := range cases {
		if got := blank(c.source, c.ips, c.wild); got != c.want {
			t.Errorf("%s %v при %v: %v", c.source, c.ips, c.wild, got)
		}
	}
}
