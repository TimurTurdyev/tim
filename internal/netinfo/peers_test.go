package netinfo

import "testing"

func TestParseLsofPeers(t *testing.T) {
	out := `COMMAND     PID   USER   FD   TYPE             DEVICE SIZE/OFF NODE NAME
launchd       1   root    7u  IPv6 0x1b2c3d4e5f6a7b8c      0t0  TCP *:22 (LISTEN)
firefox   12258 dev   75u  IPv4 0xa782e9c2d96cf0e8      0t0  TCP 10.8.1.1:53261->203.0.113.5:443 (ESTABLISHED)
Google Chrome Helper   4321 dev   67u  IPv4 0xaf083d02809e2151      0t0  TCP 192.168.3.207:53255->198.51.100.7:443 (ESTABLISHED)
Telegram  777 dev   30u  IPv4 0x00000000000000ab      0t0  UDP 10.8.1.1:60000->203.0.113.9:443
OpenVPN\x20Connect 888 dev 12u IPv4 0x00000000000000cd      0t0  TCP 192.168.3.207:40000->203.0.113.11:1194 (ESTABLISHED)
mDNSRespo   456 _mdns    9u  IPv4 0x0000000000000001      0t0  UDP *:5353
ssh       99001 dev    3u  IPv6 0x0000000000000002      0t0  TCP [fd58::1]:50000->[2001:db8::10]:22 (ESTABLISHED)
`
	got := ParseLsofPeers(out)
	want := []Peer{
		{"firefox", "10.8.1.1", "203.0.113.5"},
		{"Google Chrome Helper", "192.168.3.207", "198.51.100.7"},
		{"Telegram", "10.8.1.1", "203.0.113.9"},
		{"OpenVPN Connect", "192.168.3.207", "203.0.113.11"},
		{"ssh", "fd58::1", "2001:db8::10"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d peers, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("peer %d: got %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestParseSSPeers(t *testing.T) {
	out := `tcp   ESTAB  0      0      10.8.0.2:52345    203.0.113.5:443    users:(("chrome",pid=4242,fd=51))
tcp   LISTEN 0      128    0.0.0.0:22        0.0.0.0:*          users:(("sshd",pid=1,fd=3))
udp   UNCONN 0      0      0.0.0.0:68        0.0.0.0:*
tcp   ESTAB  0      0      [fd58::2]:4000    [2001:db8::10]:443 users:(("curl",pid=7,fd=5))
`
	got := ParseSSPeers(out)
	want := []Peer{
		{"chrome", "10.8.0.2", "203.0.113.5"},
		{"curl", "fd58::2", "2001:db8::10"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d peers, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("peer %d: got %+v, want %+v", i, got[i], want[i])
		}
	}
}
