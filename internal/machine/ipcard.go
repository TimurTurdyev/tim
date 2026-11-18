package machine

import (
	"context"
	"fmt"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"github.com/TimurTurdyev/tim/internal/asn"
	"github.com/TimurTurdyev/tim/internal/dnsbl"
	"github.com/TimurTurdyev/tim/internal/dnsq"
	"github.com/TimurTurdyev/tim/internal/domaudit"
	"github.com/TimurTurdyev/tim/internal/inspect"
	"github.com/TimurTurdyev/tim/internal/internetdb"
	"github.com/TimurTurdyev/tim/internal/level"
	"github.com/TimurTurdyev/tim/internal/netinfo"
	"github.com/TimurTurdyev/tim/internal/par"
	"github.com/TimurTurdyev/tim/internal/reputation"
	"github.com/TimurTurdyev/tim/internal/ui"
	"github.com/TimurTurdyev/tim/report"
)

const (
	ipLookupTimeout = 4 * time.Second
	ipNeighborsMax  = 12
)

// ipCard: чужой адрес - чей он, где стоит, что на нём видно и в каких он списках.
type ipCard struct {
	IP     string   `json:"ip"`
	Name   string   `json:"name,omitempty"`   // спросили по имени
	Others []string `json:"others,omitempty"` // остальные адреса имени
	// Internal: адрес не из интернета; CDN: узел раздачи - то, что на нём, не от этого сайта.
	Internal  bool                  `json:"internal,omitempty"`
	CDN       bool                  `json:"cdn,omitempty"`
	AS        asn.ASInfo            `json:"as,omitzero"`
	PTR       string                `json:"ptr,omitempty"`
	Geo       *inspect.Geo          `json:"geo,omitempty"`
	Services  *internetdb.Info      `json:"services,omitempty"`
	Sites     domaudit.Neighborhood `json:"sites,omitzero"`
	Blacklist []dnsbl.Result        `json:"blacklist,omitempty"`
	Lists     []string              `json:"lists,omitempty"` // списки массового шума
	Noise     *reputation.Noise     `json:"greynoise,omitempty"`
	Errors    []string              `json:"errors,omitempty"`

	listsLoaded bool
}

// ipCardReport: карточка адреса или имени.
func ipCardReport(arg string) (report.Report, error) {
	c, err := newIPCard(arg)
	if err != nil {
		return report.Report{}, err
	}
	if !c.Internal {
		c.collect()
	}
	return c.report(), nil
}

// newIPCard: адрес из аргумента; у имени - первый публичный, остальные рядом.
func newIPCard(arg string) (ipCard, error) {
	if a, err := netip.ParseAddr(strings.Trim(arg, "[]")); err == nil {
		ip := a.Unmap().WithZone("").String()
		return ipCard{IP: ip, Internal: !netinfo.IsPublic(ip)}, nil
	}
	host, _ := netinfo.HostPort(arg, "")
	t, err := netinfo.ParseTarget(host)
	switch {
	case err == nil && t.Kind == netinfo.KindHost:
		return ipCard{IP: t.Value, Internal: !netinfo.IsPublic(t.Value)}, nil
	case err != nil || t.Kind != netinfo.KindDomain:
		return ipCard{}, ui.Errorf("%s - не адрес и не имя: tim ip 1.1.1.1 или tim ip example.com", arg)
	}
	ctx, cancel := context.WithTimeout(context.Background(), ipLookupTimeout)
	defer cancel()
	found, err := dnsq.Resolver().LookupNetIP(ctx, "ip", t.Value)
	if err != nil || len(found) == 0 {
		return ipCard{}, ui.Errorf("%s: такого имени в DNS нет", t.Value)
	}
	c := ipCard{Name: t.Value, Internal: true}
	for _, a := range found {
		ip := a.Unmap().String()
		// Публичный IPv4 важнее: про него знают все источники.
		if c.IP == "" || c.Internal && netinfo.IsPublic(ip) || !netinfo.IsIPv4(c.IP) && netinfo.IsIPv4(ip) && netinfo.IsPublic(ip) {
			if c.IP != "" {
				c.Others = append(c.Others, c.IP)
			}
			c.IP, c.Internal = ip, !netinfo.IsPublic(ip)
			continue
		}
		c.Others = append(c.Others, ip)
	}
	return c, nil
}

// collect: сначала чья сеть и обратное имя; у узла CDN на этом и всё - остальное было бы про чужие сайты.
func (c *ipCard) collect() {
	par.All(
		func() { c.AS = asn.Cymru([]string{c.IP})[c.IP] },
		func() { c.PTR = reverse(c.IP) },
	)
	c.CDN = asn.IsEdge(c.AS.Name, c.PTR)
	if c.CDN {
		return
	}
	var geoErr, scanErr, sitesErr error
	v4 := netinfo.IsIPv4(c.IP)
	tasks := []func(){
		func() { c.Geo, geoErr = inspect.IPGeo(c.IP) },
		func() {
			lists := reputation.Load()
			c.listsLoaded, c.Lists = !lists.Empty(), lists.In(c.IP)
		},
	}
	if v4 {
		tasks = append(tasks,
			func() {
				info, err := internetdb.Look(c.IP)
				if scanErr = err; err == nil && !info.Empty() {
					c.Services = &info
				}
			},
			func() { c.Sites, sitesErr = domaudit.Neighbors(c.IP, "") },
			func() { c.Blacklist = dnsbl.Check(c.IP) },
			func() {
				if n, ok := reputation.GreyNoise(c.IP); ok {
					c.Noise = &n
				}
			},
		)
	}
	par.All(tasks...)
	for _, e := range []struct {
		what string
		err  error
	}{{"место", geoErr}, {"порты", scanErr}, {"сайты на адресе", sitesErr}} {
		if e.err != nil {
			c.Errors = append(c.Errors, e.what+": "+ui.Plain(ui.ShortErr(e.err)))
		}
	}
	if c.Services != nil && c.Services.CDN() {
		c.CDN, c.Services, c.Sites = true, nil, domaudit.Neighborhood{}
	}
}

func reverse(ip string) string {
	ctx, cancel := context.WithTimeout(context.Background(), ipLookupTimeout)
	defer cancel()
	names, err := dnsq.Resolver().LookupAddr(ctx, ip)
	if err != nil || len(names) == 0 {
		return ""
	}
	return strings.TrimSuffix(names[0], ".")
}

func (c ipCard) report() report.Report {
	r := report.Report{Schema: "tim.ip.card/1", Title: c.IP, Data: c}
	if c.Name != "" {
		r.Title = c.Name + " · " + c.IP
	}
	if c.Internal {
		r.Verdict = report.Verdict{Level: level.Info, Text: "не из интернета"}
		facts := report.Facts{{Key: "адрес", Value: "внутренний или служебный: в интернете за ним никого, наружу не спрашиваем"}}
		if len(c.Others) > 0 {
			facts = append(facts, report.Fact{Key: "ещё адреса", Value: strings.Join(c.Others, ", ")})
		}
		r.Blocks = []report.Block{facts}
		return r
	}
	worst := level.Skip
	facts := c.facts()
	for _, f := range facts {
		worst = level.Worst(worst, f.Level)
	}
	switch {
	case c.CDN:
		r.Verdict = report.Verdict{Level: level.Info, Text: "узел CDN"}
	case worst >= level.Warn:
		r.Verdict = report.Verdict{Level: worst, Text: "есть в списках"}
	}
	r.Blocks = []report.Block{facts}
	if c.Services != nil {
		r.Blocks = append(r.Blocks, report.Note("порты и версии - по данным Shodan, до недели назад; к самому адресу запросов не было"))
	}
	for _, e := range c.Errors {
		r.Blocks = append(r.Blocks, report.Note("не узнали: "+e))
	}
	if c.Name != "" {
		r.Blocks = append(r.Blocks, report.Next{{Text: "домен целиком", Command: "tim domain " + c.Name}})
	}
	return r
}

func (c ipCard) facts() report.Facts {
	var f report.Facts
	add := func(key, value string, l level.Level) {
		if value != "" {
			f = append(f, report.Fact{Key: key, Value: value, Level: l})
		}
	}
	add("ещё адреса", strings.Join(c.Others, ", "), level.Info)
	network := c.AS.Owner()
	for _, part := range []string{prefixed("AS", c.AS.ASN), c.AS.Prefix} {
		if part != "" {
			network = strings.TrimPrefix(network+" · "+part, " · ")
		}
	}
	add("сеть", network, level.Skip)
	if g := c.Geo; g != nil {
		add("где", strings.Join(nonBlank(g.Country, g.City), ", "), level.Skip)
		if g.ISP != "" && g.ISP != c.AS.Owner() {
			add("провайдер", g.ISP, level.Skip)
		}
	}
	add("PTR", c.PTR, level.Skip)
	if c.CDN {
		add("CDN", "адрес раздаёт много чужих сайтов, настоящий сервер за ним не виден: порты, соседи и списки ничего не скажут", level.Skip)
		return f
	}
	if s := c.Services; s != nil {
		ports := make([]string, len(s.Ports))
		for i, p := range s.Ports {
			ports[i] = strconv.Itoa(p)
		}
		add("порты", strings.Join(ports, " "), level.Skip)
		add("что отвечает", strings.Join(s.Products, ", "), level.Skip)
		if n := len(s.CVE); n > 0 {
			add("CVE", fmt.Sprintf("%d по номеру версии: Debian и Ubuntu чинят, не меняя номер", n), level.Info)
		}
	} else if netinfo.IsIPv4(c.IP) {
		add("порты", "сканеры Shodan ничего не видели", level.Info)
	}
	if n := c.Sites; len(n.Names) > 0 || n.Seen > 0 {
		add("сайты", sitesText(n), level.Skip)
	}
	text, l := c.blacklistText()
	add("почта", text, l)
	text, l = c.noiseText()
	add("шум", text, l)
	return f
}

func sitesText(n domaudit.Neighborhood) string {
	text := "сейчас ни одного"
	if len(n.Names) > 0 {
		shown := n.Names[:min(len(n.Names), ipNeighborsMax)]
		text = strings.Join(shown, ", ")
		if rest := len(n.Names) - len(shown); rest > 0 {
			text += " и ещё " + strconv.Itoa(rest)
		}
	}
	if n.Seen > len(n.Names) {
		text += fmt.Sprintf(" (за всё время видели %d)", n.Seen)
	}
	return text
}

// blacklistText: чёрные списки почты; кто не ответил, о том не говорим "чисто".
func (c ipCard) blacklistText() (string, level.Level) {
	var listed, unknown []string
	for _, b := range c.Blacklist {
		switch {
		case b.Listed:
			listed = append(listed, b.Zone)
		case b.Unknown != "":
			unknown = append(unknown, b.Zone+" "+b.Unknown)
		}
	}
	switch {
	case len(c.Blacklist) == 0:
		return "", level.Skip
	case len(listed) > 0:
		return "в чёрных списках: " + strings.Join(listed, ", "), level.Bad
	case len(unknown) > 0:
		return "в чёрных списках нет; " + strings.Join(unknown, ", "), level.Skip
	}
	return "в чёрных списках нет", level.Skip
}

// noiseText: сканирует ли адрес всех подряд.
func (c ipCard) noiseText() (string, level.Level) {
	var parts []string
	l := level.Skip
	if len(c.Lists) > 0 {
		parts, l = append(parts, "в списках "+strings.Join(c.Lists, ", ")), level.Warn
	}
	if n := c.Noise; n != nil {
		switch {
		case n.RIOT:
			parts = append(parts, "GreyNoise: известный сервис "+ui.Plain(n.Name))
		case n.Noise:
			parts, l = append(parts, "GreyNoise: сканирует весь интернет"), level.Warn
		}
	}
	if len(parts) > 0 {
		return strings.Join(parts, "; "), l
	}
	if !c.listsLoaded {
		return "списки не скачались", level.Info
	}
	return "в списках массовых сканеров нет", level.Skip
}

func prefixed(prefix, s string) string {
	if s == "" {
		return ""
	}
	return prefix + s
}

func nonBlank(list ...string) []string {
	var out []string
	for _, s := range list {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}
