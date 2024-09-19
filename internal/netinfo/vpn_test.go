package netinfo

import "testing"

const ifconfigSample = `lo0: flags=8049<UP,LOOPBACK,RUNNING,MULTICAST> mtu 16384
	inet 127.0.0.1 netmask 0xff000000
en0: flags=8863<UP,BROADCAST,SMART,RUNNING,SIMPLEX,MULTICAST> mtu 1500
	inet 192.0.2.10 netmask 0xffffff00 broadcast 192.0.2.255
	status: active
utun0: flags=8051<UP,POINTOPOINT,RUNNING,MULTICAST> mtu 1500
	inet6 fe80::3433:9fce:936:d3d%utun0 prefixlen 64 scopeid 0x12
	nd6 options=201<PERFORMNUD,DAD>
utun4: flags=8051<UP,POINTOPOINT,RUNNING,MULTICAST> mtu 1420
	inet 10.66.0.2 --> 10.66.0.2 netmask 0xffffffff
	inet6 fd00:66::2 prefixlen 64
utun5: flags=8051<UP,POINTOPOINT,RUNNING,MULTICAST> mtu 1380
	inet6 fe80::1%utun5 prefixlen 64 scopeid 0x17
`

func TestParseIfconfigTunnels(t *testing.T) {
	list := ParseIfconfigTunnels(ifconfigSample)
	if len(list) != 1 || list[0].Iface != "utun4" || list[0].IPv4[0] != "10.66.0.2" || list[0].IPv6[0] != "fd00:66::2" {
		t.Errorf("tunnels: %+v", list)
	}
}

const ncSample = `Available network connection services in the current set (*=enabled):
* (Connected)      1A2B3C4D-0000-0000-0000-000000000001 IPSec              "Work VPN"                       [IPSec]
* (Disconnected)   1A2B3C4D-0000-0000-0000-000000000002 VPN (com.wireguard.macos) "Home WG"         [VPN/com.wireguard.macos]
`

func TestParseNcList(t *testing.T) {
	list := ParseNcList(ncSample)
	if len(list) != 2 || list[0].Name != "Work VPN" || list[0].State != "Connected" || list[1].State != "Disconnected" {
		t.Errorf("nc: %+v", list)
	}
}

const dnsSample = `DNS configuration

resolver #1
  search domain[0] : lan
  nameserver[0] : 192.0.2.1
  nameserver[1] : 198.51.100.53
  flags    : Request A records
  order    : 5000

resolver #2
  domain   : local
  options  : mdns

DNS configuration (for scoped queries)

resolver #1
  nameserver[0] : 203.0.113.53
`

const dnsVPNFirst = `DNS configuration

resolver #1
  domain   : corp.example
  nameserver[0] : 10.66.0.1

resolver #2
  nameserver[0] : 10.66.0.53
`

func TestParseDNS(t *testing.T) {
	got := ParseDNS(dnsSample)
	if len(got) != 2 || got[0] != "192.0.2.1" || got[1] != "198.51.100.53" {
		t.Errorf("dns: %v", got)
	}
	got = ParseDNS(dnsVPNFirst)
	if len(got) != 1 || got[0] != "10.66.0.53" {
		t.Errorf("dns vpn: %v", got)
	}
}

func TestParseSocksProxy(t *testing.T) {
	p := ParseSocksProxy("Enabled: Yes\nServer: 127.0.0.1\nPort: 1080\nAuthenticated Proxy Enabled: 0\n")
	if !p.Enabled || p.Server != "127.0.0.1" || p.Port != "1080" {
		t.Errorf("proxy %+v", p)
	}
	if ParseSocksProxy("Enabled: No\nServer: \nPort: 0\n").Enabled {
		t.Error("should be off")
	}
}
