package sshx

import (
	"strings"
	"testing"
)

func TestParseForward(t *testing.T) {
	cases := map[string]Forward{
		"5432":                {"5432", "localhost", "5432"},
		"15432:5432":          {"15432", "localhost", "5432"},
		"8080:db.internal:80": {"8080", "db.internal", "80"},
	}
	for in, want := range cases {
		got, err := ParseForward(in)
		if err != nil || got != want {
			t.Errorf("%s: %+v %v", in, got, err)
		}
	}
	if a := cases["8080:db.internal:80"].Arg(); a != "127.0.0.1:8080:db.internal:80" {
		t.Errorf("arg %s", a)
	}
	for _, bad := range []string{"", "0", "70000", "a:b", "1:2:3:4", "80::80"} {
		if _, err := ParseForward(bad); err == nil {
			t.Errorf("%q should fail", bad)
		}
	}
}

func TestShuttleArgs(t *testing.T) {
	got := ShuttleArgs("vps", "203.0.113.10", true, []string{"198.51.100.0/24"}, "/tmp/s.pid")
	want := "-r vps 0/0 --dns -x 203.0.113.10/32 -x 10.0.0.0/8 -x 172.16.0.0/12 -x 192.168.0.0/16 -x 198.51.100.0/24 -D --pidfile /tmp/s.pid"
	if j := strings.Join(got, " "); j != want {
		t.Errorf("got  %s\nwant %s", j, want)
	}
	if j := strings.Join(ShuttleArgs("vps", "", false, nil, "p"), " "); j != "-r vps 0/0 -x 10.0.0.0/8 -x 172.16.0.0/12 -x 192.168.0.0/16 -D --pidfile p" {
		t.Errorf("minimal: %s", j)
	}
}
