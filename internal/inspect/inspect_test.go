package inspect

import "testing"

const rdapSample = `{"objectClassName":"domain","ldhName":"EXAMPLE.COM",
"events":[{"eventAction":"registration","eventDate":"1995-08-14T04:00:00Z"},
{"eventAction":"expiration","eventDate":"2027-08-13T04:00:00Z"},
{"eventAction":"last update of RDAP database","eventDate":"2025-10-18T10:00:00Z"}],
"nameservers":[{"ldhName":"A.IANA-SERVERS.NET"},{"ldhName":"B.IANA-SERVERS.NET"}],
"entities":[{"roles":["registrar"],"vcardArray":["vcard",[["version",{},"text","4.0"],["fn",{},"text","RESERVED-Internet Assigned Numbers Authority"]]]}]}`

func TestParseRDAP(t *testing.T) {
	d, err := ParseRDAP([]byte(rdapSample))
	if err != nil {
		t.Fatal(err)
	}
	if d.Registrar != "RESERVED-Internet Assigned Numbers Authority" {
		t.Errorf("registrar %q", d.Registrar)
	}
	if d.Created.Year() != 1995 || d.Expires.Year() != 2027 {
		t.Errorf("dates %v %v", d.Created, d.Expires)
	}
	if len(d.Nameservers) != 2 || d.Nameservers[0] != "a.iana-servers.net" {
		t.Errorf("ns %v", d.Nameservers)
	}
}

const whoisRu = `% TCI Whois Service. Terms of use:
domain:        EXAMPLE.RU
nserver:       ns1.example.net.
nserver:       ns2.example.net.
state:         REGISTERED, DELEGATED, VERIFIED
org:           Example LLC
registrar:     RU-CENTER-RU
admin-contact: https://www.nic.ru/whois
created:       2004-02-11T12:00:00Z
paid-till:     2027-02-11T21:00:00Z
free-date:     2027-03-15
source:        TCI
`

const whoisCom = `   Domain Name: EXAMPLE.ORG
   Registrar: Example Registrar, Inc.
   Creation Date: 2001-05-02T10:00:00Z
   Registry Expiry Date: 2028-05-02T10:00:00Z
   Name Server: NS1.EXAMPLE.ORG
   Name Server: NS2.EXAMPLE.ORG
`

func TestParseWhois(t *testing.T) {
	ru := ParseWhois(whoisRu)
	if ru.Registrar != "RU-CENTER-RU" || ru.Created.Year() != 2004 || ru.Expires.Year() != 2027 {
		t.Errorf("ru: %+v", ru)
	}
	if len(ru.Nameservers) != 2 || ru.Nameservers[1] != "ns2.example.net" {
		t.Errorf("ru ns: %v", ru.Nameservers)
	}
	com := ParseWhois(whoisCom)
	if com.Registrar != "Example Registrar, Inc." || com.Expires.Year() != 2028 || com.Nameservers[0] != "ns1.example.org" {
		t.Errorf("com: %+v", com)
	}
}
