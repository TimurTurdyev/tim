package domaudit

import (
	"slices"
	"strconv"
	"strings"

	"github.com/TimurTurdyev/tim/internal/netinfo"
	"github.com/TimurTurdyev/tim/internal/par"
	"github.com/TimurTurdyev/tim/internal/portscan"
)

const (
	maxChecks  = 20 // адресов, чьи порты проверяем сами
	checkWidth = 8
)

// check: адрес, который надо проверить, и имя для HTTP и TLS.
type check struct {
	addr *Addr
	host string
}

// Check проверяет порты адреса сами, без чужих данных.
func Check(a *Addr) portscan.Result {
	r := portscan.Scan(a.IP, a.host())
	a.Checked, a.Open = !r.Faked, r.Services
	return r
}

// host: каким именем назваться серверу; пусто - адресом.
func (a Addr) host() string {
	if len(a.Hosts) > 0 {
		return a.Hosts[0]
	}
	return ""
}

// Ports: порты адреса. Что проверили сами, то и есть; сверх нашего списка - по данным Shodan.
func (a Addr) Ports() []int {
	var out []int
	for _, s := range a.Open {
		out = append(out, s.Port)
	}
	if a.Services != nil {
		for _, p := range a.Services.Ports {
			if !a.Checked || !portscan.Covers(p) {
				out = append(out, p)
			}
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// Answers: кто отвечает на портах словами.
func (a Addr) Answers() []string {
	var out []string
	for _, s := range a.Open {
		text := s.Name
		switch {
		case s.Silent:
			text = strconv.Itoa(s.Port) + " молчит"
		case s.Product != "":
			text += " " + s.Product
		}
		if !slices.Contains(out, text) {
			out = append(out, text)
		}
	}
	if !a.Checked && a.Services != nil {
		out = append(out, a.Services.Products...)
	}
	return out
}

// unchecked: свои адреса, чьи порты ещё не проверяли.
func (c *collector) unchecked() []check {
	if !c.opt.Scan {
		return nil
	}
	var list []check
	for _, a := range c.addrs {
		if a.Own && !a.tried && netinfo.IsIPv4(a.IP) {
			list = append(list, check{a, a.host()})
		}
	}
	slices.SortFunc(list, func(x, y check) int { return strings.Compare(x.addr.IP, y.addr.IP) })
	for _, x := range list {
		x.addr.tried = true
	}
	room := max(0, maxChecks-c.checked)
	c.checked += len(list)
	return list[:min(len(list), room)]
}

// checkPorts: порты своих адресов; сеть, которая отвечает за всех, не засчитывается.
func (c *collector) checkPorts(list []check) {
	for i, r := range par.Map(list, checkWidth, func(x check) portscan.Result { return portscan.Scan(x.addr.IP, x.host) }) {
		list[i].addr.Checked, list[i].addr.Open = !r.Faked, r.Services
		if r.Intercepted {
			c.say("порты: сеть на пути принимает любое соединение (VPN), показаны те, где сервис назвался")
		}
		if r.Faked {
			c.say("порты не проверены: за серверы отвечает кто-то на пути")
		}
	}
}
