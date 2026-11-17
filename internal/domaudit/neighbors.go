package domaudit

import (
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/TimurTurdyev/tim/internal/cache"
	"github.com/TimurTurdyev/tim/internal/inspect"
	"github.com/TimurTurdyev/tim/internal/netinfo"
	"github.com/TimurTurdyev/tim/internal/par"
	"github.com/TimurTurdyev/tim/internal/paths"
	"github.com/TimurTurdyev/tim/internal/ui"
)

const (
	maxNeighborAddrs = 20
	neighborAsk      = 100 // сколько имён просить у источника
	neighborCheck    = 50  // сколько из них перепроверить
	neighborTTL      = 20 * time.Hour
	neighborTimeout  = 12 * time.Second
)

// Neighborhood: чужие сайты адреса. Names - те, что и сейчас на нём; Seen - сколько имён источник видел на адресе за
// всё время; Source - кто ответил.
type Neighborhood struct {
	Names  []string `json:"names,omitempty"`
	Seen   int      `json:"seen,omitempty"`
	Source string   `json:"source,omitempty"`
}

// Neighbors: чужие сайты на адресе. Источники помнят прошлое, поэтому имена перепроверяются: остаются те, что
// резолвятся в этот же адрес. self - чей адрес: имена того же домена не соседи; пусто - показать все.
func Neighbors(ip, self string) (Neighborhood, error) {
	return cache.Get(cache.Path(paths.State("neighbors"), ip+"_"+self), neighborTTL, func() (Neighborhood, error) {
		n, err := askTHC(ip)
		if err != nil {
			var spare error
			if n, spare = askReverseIP(ip); spare != nil {
				return Neighborhood{}, errors.Join(err, spare)
			}
		}
		n.Names = onePerSite(foreign(n.Names, ip, self), neighborCheck)
		n.Names = stillHere(n.Names, ip)
		return n, nil
	})
}

// unmet: свои адреса, у которых ещё не спрашивали соседей, - сначала адреса самого домена.
func (c *collector) unmet() []*Addr {
	if c.opt.Quiet || c.opt.SkipNeighbors {
		return nil
	}
	var list []*Addr
	for _, a := range c.addrs {
		if a.Own && !a.met && netinfo.IsIPv4(a.IP) {
			list = append(list, a)
		}
	}
	apex := func(a *Addr) int {
		if slices.Contains(a.Hosts, c.domain) {
			return 0
		}
		return 1
	}
	slices.SortFunc(list, func(a, b *Addr) int { return cmp.Or(apex(a)-apex(b), strings.Compare(a.IP, b.IP)) })
	for _, a := range list {
		a.met = true
	}
	room := max(0, maxNeighborAddrs-c.met)
	c.met += len(list)
	return list[:min(len(list), room)]
}

// neighbors: соседи адресов. Запасной источник с дневным лимитом берётся только на первый адрес: молчит основной -
// остальные ждут следующего раза.
func (c *collector) neighbors(list []*Addr) {
	if len(list) == 0 || c.alone {
		return
	}
	first, err := Neighbors(list[0].IP, c.domain)
	if err != nil {
		c.alone = true
		c.fail("соседи: " + ui.Plain(strings.ReplaceAll(err.Error(), "\n", ", ")))
		return
	}
	list[0].Shared, list[0].SharedSeen = first.Names, first.Seen
	if c.alone = first.Source != sourceTHC; c.alone {
		return
	}
	for i, n := range par.Map(list[1:], scanWidth, func(a *Addr) Neighborhood {
		n, _ := Neighbors(a.IP, c.domain)
		return n
	}) {
		list[i+1].Shared, list[i+1].SharedSeen = n.Names, n.Seen
	}
}

func askTHC(ip string) (Neighborhood, error) {
	body, err := json.Marshal(map[string]any{"ip_address": ip, "limit": neighborAsk})
	if err != nil {
		return Neighborhood{}, err
	}
	resp, err := (&http.Client{Timeout: neighborTimeout}).Post("https://ip.thc.org/api/v1/lookup", "application/json", bytes.NewReader(body))
	if err != nil {
		return Neighborhood{}, netError(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return Neighborhood{}, errors.New("THC ответил " + ui.Plain(resp.Status))
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return Neighborhood{}, netError(err)
	}
	return ParseTHCNeighbors(data)
}

// ParseTHCNeighbors разбирает ответ THC про адрес: имена и сколько их всего.
func ParseTHCNeighbors(data []byte) (Neighborhood, error) {
	var raw struct {
		Total   int `json:"matching_records"`
		Domains []struct {
			Domain string `json:"domain"`
		} `json:"domains"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return Neighborhood{}, errNotJSON
	}
	n := Neighborhood{Seen: raw.Total, Source: sourceTHC}
	for _, d := range raw.Domains {
		if name := strings.ToLower(d.Domain); hostLine.MatchString(name) {
			n.Names = append(n.Names, name)
		}
	}
	return n, nil
}

func askReverseIP(ip string) (Neighborhood, error) {
	r, err := get("https://api.hackertarget.com/reverseiplookup/?q="+url.QueryEscape(ip), neighborTimeout)
	if err != nil {
		return Neighborhood{}, err
	}
	names := ParseNeighbors(string(r.body), "")
	return Neighborhood{Names: names, Seen: len(names), Source: sourceHackertarget}, nil
}

// foreign: имена чужих сайтов - не того же домена и не имя, которое хостинг составил из самого адреса.
func foreign(names []string, ip, self string) []string {
	zone := inspect.Registrable(self)
	dashed := strings.ReplaceAll(ip, ".", "-")
	return slices.DeleteFunc(slices.Clone(names), func(n string) bool {
		return self != "" && inspect.Registrable(n) == zone || strings.Contains(n, dashed) || strings.Contains(n, ip)
	})
}

// onePerSite: не больше limit имён, сначала по одному от каждого сайта: сорок поддоменов одного соседа - это один сосед.
func onePerSite(names []string, limit int) []string {
	names = slices.Clone(names)
	slices.SortFunc(names, func(a, b string) int { return cmp.Or(cmp.Compare(len(a), len(b)), strings.Compare(a, b)) })
	names = slices.Compact(names)
	seen := map[string]bool{}
	var first, rest []string
	for _, n := range names {
		if site := inspect.Registrable(n); !seen[site] {
			seen[site] = true
			first = append(first, n)
			continue
		}
		rest = append(rest, n)
	}
	first = append(first, rest...)
	return first[:min(len(first), limit)]
}

// stillHere: имена, которые и сейчас резолвятся в этот адрес.
func stillHere(names []string, ip string) []string {
	var out []string
	for i, ips := range par.Map(names, neighborCheck, lookup) {
		if slices.Contains(ips, ip) {
			out = append(out, names[i])
		}
	}
	slices.Sort(out)
	return out
}
