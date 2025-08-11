package sshx

import (
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
