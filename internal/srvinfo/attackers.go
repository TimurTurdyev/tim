package srvinfo

import (
	"cmp"
	"fmt"
	"net/netip"
	"slices"
	"strings"

	"github.com/TimurTurdyev/tim/internal/ui"
)

// Пороги "прицельности": меньше - фоновый шум, такого в интернете много у любого сервера.
const (
	targetSSH    = 20
	targetProbes = 5
)

// Attacker: адрес и что он делал за окно.
type Attacker struct {
	IP     string   `json:"ip"`
	SSH    int      `json:"ssh"`    // неудачных попыток входа
	Probes int      `json:"probes"` // проб сайта
	Paths  []string `json:"paths,omitempty"`
	Sites  []string `json:"sites,omitempty"`
	UFW    int      `json:"ufw"`
	Lists  []string `json:"lists,omitempty"` // FireHOL, Spamhaus DROP, Tor
	Noise  bool     `json:"noise"`           // GreyNoise: массовый сканер
}

// NoiseSum: шум одной строкой.
type NoiseSum struct {
	IPs      int      `json:"ips"`
	Attempts int      `json:"attempts"`
	Sources  []string `json:"sources,omitempty"`
}

// ByIP: адреса со всех источников с подробностями, самые активные первыми.
func ByIP(a Attacks) []Attacker {
	m := map[string]*Attacker{}
	get := func(ip string) *Attacker {
		if m[ip] == nil {
			m[ip] = &Attacker{IP: ip}
		}
		return m[ip]
	}
	for _, c := range a.SSHByIP {
		get(c.Key).SSH += c.N
	}
	for _, c := range a.UFWByIP {
		get(c.Key).UFW += c.N
	}
	for _, p := range a.Probes {
		at := get(p.IP)
		at.Probes++
		if !slices.Contains(at.Paths, p.Path) {
			at.Paths = append(at.Paths, p.Path)
		}
		if s := siteName(p.File); !slices.Contains(at.Sites, s) {
			at.Sites = append(at.Sites, s)
		}
	}
	out := make([]Attacker, 0, len(m))
	for _, at := range m {
		out = append(out, *at)
	}
	slices.SortFunc(out, func(x, y Attacker) int { return cmp.Or(cmp.Compare(y.total(), x.total()), cmp.Compare(x.IP, y.IP)) })
	return out
}

func (a Attacker) total() int { return a.SSH + a.Probes + a.UFW }

// Candidate: стоит ли спрашивать о нём GreyNoise - заметная активность и ни в одном списке.
func (a Attacker) Candidate() bool {
	return len(a.Lists) == 0 && (a.SSH >= targetSSH || a.Probes >= targetProbes)
}

// Split: прицельные - заметная активность, не в списках и не массовый сканер по GreyNoise; остальное - шум.
func Split(list []Attacker) (targeted []Attacker, noise NoiseSum) {
	sources := map[string]bool{}
	for _, a := range list {
		if a.Candidate() && !a.Noise {
			targeted = append(targeted, a)
			continue
		}
		noise.IPs++
		noise.Attempts += a.total()
		for _, l := range a.Lists {
			sources[l] = true
		}
		if a.Noise {
			sources["GreyNoise"] = true
		}
	}
	for _, s := range []string{"GreyNoise", "FireHOL", "Spamhaus DROP", "Tor"} {
		if sources[s] {
			noise.Sources = append(noise.Sources, s)
		}
	}
	return targeted, noise
}

// Doing: что адрес делал - для строки ЦЕЛЕНАПРАВЛЕННО.
func (a Attacker) Doing() string {
	var parts []string
	if a.SSH > 0 {
		parts = append(parts, fmt.Sprintf("%d %s пароля", a.SSH, ui.Plural(a.SSH, "попытка", "попытки", "попыток")))
	}
	if a.Probes > 0 {
		paths := strings.Join(a.Paths[:min(2, len(a.Paths))], " и ")
		parts = append(parts, fmt.Sprintf("%s на %s · %d %s", paths, strings.Join(a.Sites, ", "), a.Probes,
			ui.Plural(a.Probes, "запрос", "запроса", "запросов")))
	}
	if a.UFW > 0 && len(parts) == 0 {
		parts = append(parts, fmt.Sprintf("%d %s в закрытые порты", a.UFW, ui.Plural(a.UFW, "стук", "стука", "стуков")))
	}
	return strings.Join(parts, " · ")
}

// Network: сеть адреса для "впервые из этой сети" - /24 у IPv4, /48 у IPv6.
func Network(ip string) string {
	a, err := netip.ParseAddr(ip)
	if err != nil {
		return ip
	}
	bits := 24
	if a.Is6() && !a.Is4In6() {
		bits = 48
	}
	p, _ := a.Prefix(bits)
	return p.String()
}
