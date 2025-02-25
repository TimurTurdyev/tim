package netinfo

import "testing"

func TestParseNettop(t *testing.T) {
	out := `,bytes_in,bytes_out,
ssh.65257,725515,1179134842,
firefox.12157,61968,115247,
,bytes_in,bytes_out,
amneziawg-go.81961,31559,21554,
Python.14807,10579,5387,
launchd.1,0,0,
com.apple.WebKit.Networking.501,900000,100,
`
	got := ParseNettop(out)
	// Первый блок - накопленное с запуска: ssh с гигабайтом там быть не должен.
	if len(got) != 3 || got[0].Name != "com.apple.WebKit.Networking" || got[0].PID != "501" {
		t.Fatalf("%+v", got)
	}
	if got[1].Name != "amneziawg-go" || got[1].In != 31559 {
		t.Fatalf("%+v", got[1])
	}
}
