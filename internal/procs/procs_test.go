package procs

import (
	"testing"
	"time"
)

func TestParseCwd(t *testing.T) {
	out := "p1076\nfcwd\nn/\np52012\nfcwd\nn/Users/dev/Dev/projects/shop/app\n"
	m := ParseCwd(out)
	if m["1076"] != "/" || m["52012"] != "/Users/dev/Dev/projects/shop/app" || len(m) != 2 {
		t.Errorf("cwd: %v", m)
	}
}

func TestParseEtime(t *testing.T) {
	cases := map[string]time.Duration{
		"03:04":        3*time.Minute + 4*time.Second,
		"02:03:04":     2*time.Hour + 3*time.Minute + 4*time.Second,
		"13-20:50:38":  13*24*time.Hour + 20*time.Hour + 50*time.Minute + 38*time.Second,
		"  1-00:00:01": 24*time.Hour + time.Second,
	}
	for in, want := range cases {
		got, ok := ParseEtime(in)
		if !ok || got != want {
			t.Errorf("%q: %v %v", in, got, ok)
		}
	}
	for _, bad := range []string{"", "12", "a:b", "x-01:02"} {
		if _, ok := ParseEtime(bad); ok {
			t.Errorf("%q should fail", bad)
		}
	}
}

func TestParsePs(t *testing.T) {
	out := " 1718 13-20:50:15 /opt/homebrew/opt/manticoresearch/bin/searchd\n52012    05:10 php artisan serve\n"
	m := ParsePs(out)
	if m["52012"].Command != "php artisan serve" || m["52012"].Uptime != 5*time.Minute+10*time.Second {
		t.Errorf("ps: %+v", m)
	}
	if !System("/usr/libexec/rapportd") || System("/opt/homebrew/bin/php") {
		t.Error("system detection")
	}
}
