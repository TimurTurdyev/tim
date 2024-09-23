package sysinfo

import "testing"

func TestParseLaunchctl(t *testing.T) {
	out := "PID\tStatus\tLabel\n1729\t0\thomebrew.mxcl.mariadb\n-\t78\thomebrew.mxcl.postgresql@18\n1718\t0\tsh.brew.manticoresearch\n512\t0\tcom.apple.Finder\n"
	list := ParseLaunchctl(out)
	if len(list) != 3 || list[0].Name != "mariadb" || !list[0].Running() {
		t.Fatalf("%+v", list)
	}
	if list[1].Running() || list[1].Status != 78 || list[2].Name != "manticoresearch" {
		t.Errorf("%+v", list)
	}
}

func TestParseGitStatusSB(t *testing.T) {
	cases := []struct {
		in   string
		want GitState
	}{
		{"## main...origin/main\n", GitState{Branch: "main"}},
		{"## main...origin/main [ahead 2, behind 1]\n M a.go\n?? b.go\n", GitState{Branch: "main", Changes: 2, Ahead: 2, Behind: 1}},
		{"## feature/x...origin/feature/x [behind 3]\n", GitState{Branch: "feature/x", Behind: 3}},
		{"## local-only\n M a\n", GitState{Branch: "local-only", Changes: 1, NoUp: true}},
		{"## No commits yet on main\n", GitState{Branch: "main", NoUp: true}},
		{"## HEAD (no branch)\n", GitState{Branch: "HEAD", NoUp: true}},
	}
	for _, c := range cases {
		if got := ParseGitStatusSB(c.in); got != c.want {
			t.Errorf("%q: %+v", c.in, got)
		}
	}
}

func TestParsePsTop(t *testing.T) {
	out := " 45,3 /Applications/Google Chrome.app/Contents/MacOS/Google Chrome\n 12.0 /usr/bin/php\n  0.1 zsh\n"
	list := ParsePsTop(out, 2)
	if len(list) != 2 || list[0].CPU != 45.3 || list[0].Name != "Google Chrome" || list[1].Name != "php" {
		t.Errorf("%+v", list)
	}
}
