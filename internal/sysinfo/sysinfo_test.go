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
