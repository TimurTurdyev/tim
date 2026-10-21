package domaudit

import (
	"maps"
	"slices"
	"strings"

	"golang.org/x/net/dns/dnsmessage"

	"github.com/TimurTurdyev/tim/internal/dnsq"
	"github.com/TimurTurdyev/tim/internal/par"
)

// Захват поддомена: имя смотрит через CNAME на чужой сервис, а там его никто не занял. Кто займёт,
// получит сайт на этом имени - с куками и доверием основного домена. Признаки - по списку
// can-i-take-over-xyz: суффикс цели CNAME и фраза на странице, когда имя не привязано; у сервисов,
// где цель просто пропадает из DNS, фразы нет - достаточно CNAME в никуда.
var takeoverSigns = []takeoverSign{
	{"GitHub Pages", []string{"github.io"}, []string{"There isn't a GitHub Pages site here"}, false},
	{"Heroku", []string{"herokuapp.com", "herokudns.com", "herokussl.com"}, []string{"No such app", "no-such-app"}, false},
	{"Amazon S3", []string{"amazonaws.com"}, []string{"NoSuchBucket", "The specified bucket does not exist"}, false},
	{"Azure", []string{"azurewebsites.net", "cloudapp.net", "cloudapp.azure.com", "trafficmanager.net", "azureedge.net", "azurefd.net", "blob.core.windows.net"}, nil, true},
	{"Netlify", []string{"netlify.app", "netlify.com"}, []string{"Not Found - Request ID"}, false},
	{"Vercel", []string{"vercel.app", "vercel-dns.com", "now.sh"}, []string{"DEPLOYMENT_NOT_FOUND", "The deployment could not be found"}, false},
	{"Fly.io", []string{"fly.dev"}, nil, true},
	{"Shopify", []string{"myshopify.com", "shops.myshopify.com"}, []string{"Sorry, this shop is currently unavailable"}, false},
	{"Ghost", []string{"ghost.io"}, []string{"The thing you were looking for is no longer here"}, false},
	{"Surge", []string{"surge.sh"}, []string{"project not found"}, false},
	{"Tumblr", []string{"tumblr.com"}, []string{"There's nothing here", "Whatever you were looking for doesn't currently exist"}, false},
	{"WordPress.com", []string{"wordpress.com"}, []string{"Do you want to register"}, false},
	{"Zendesk", []string{"zendesk.com"}, []string{"Help Center Closed"}, false},
	{"Unbounce", []string{"unbouncepages.com"}, []string{"The requested URL was not found on this server"}, false},
	{"Pantheon", []string{"pantheonsite.io"}, []string{"The gods are wise"}, false},
	{"Readme.io", []string{"readme.io"}, []string{"Project doesnt exist", "Project doesn't exist"}, false},
	{"Bitbucket", []string{"bitbucket.io"}, []string{"Repository not found"}, false},
	{"Fastly", []string{"fastly.net"}, []string{"Fastly error: unknown domain"}, false},
	{"Cargo", []string{"cargocollective.com"}, []string{"404 Not Found"}, false},
	{"Helpjuice", []string{"helpjuice.com"}, []string{"We could not find what you're looking for"}, false},
	{"Help Scout", []string{"helpscoutdocs.com"}, []string{"No settings were found for this company"}, false},
	{"Tilda", []string{"tilda.ws"}, []string{"Please renew your subscription", "Domain has been assigned"}, false},
	{"Webflow", []string{"proxy-ssl.webflow.com", "proxy.webflow.com"}, []string{"The page you are looking for doesn't exist or has been moved"}, false},
	{"Strikingly", []string{"s.strikinglydns.com"}, []string{"But if you're looking to build your own website"}, false},
	{"Uptime Robot", []string{"stats.uptimerobot.com"}, []string{"page not found"}, false},
	{"Agile CRM", []string{"agilecrm.com"}, []string{"Sorry, this page is no longer available"}, false},
}

// takeoverSign: сервис, суффиксы цели CNAME, фразы страницы без владельца; nx - у сервиса имя
// просто исчезает из DNS.
type takeoverSign struct {
	service string
	cname   []string
	body    []string
	nx      bool
}

// Takeover: имя можно захватить - по CNAME и ответу сервиса.
type Takeover struct {
	Service string `json:"service"`
	Target  string `json:"target"`
	Why     string `json:"why"`
}

func (s takeoverSign) matches(target string) bool {
	target = strings.TrimSuffix(strings.ToLower(target), ".")
	for _, c := range s.cname {
		if target == c || strings.HasSuffix(target, "."+c) {
			return true
		}
	}
	return false
}

// MatchTakeover: можно ли захватить имя с таким CNAME: цель не резолвится у сервиса, где имена
// пропадают из DNS, или страница сервиса говорит, что имя не привязано. ok = false, если сервис
// неизвестен или занят.
func MatchTakeover(target string, resolves bool, body string) (Takeover, bool) {
	if target == "" {
		return Takeover{}, false
	}
	for _, s := range takeoverSigns {
		if !s.matches(target) {
			continue
		}
		t := Takeover{Service: s.service, Target: strings.TrimSuffix(target, ".")}
		switch {
		case !resolves && (s.nx || len(s.body) == 0):
			t.Why = "цель CNAME не существует: имя у сервиса свободно"
			return t, true
		case !resolves:
			t.Why = "цель CNAME не резолвится"
			return t, true
		}
		for _, phrase := range s.body {
			if strings.Contains(body, phrase) {
				t.Why = "сервис отвечает, что имя ни к чему не привязано"
				return t, true
			}
		}
		return Takeover{}, false
	}
	return Takeover{}, false
}

// CNAME: куда в итоге ведёт имя - конец цепочки, если резолвер её раскрыл; пусто - прямая запись
// или имени нет.
func CNAME(name string) string {
	a, err := dnsq.Query(name, dnsmessage.TypeCNAME)
	if err != nil {
		return ""
	}
	target := ""
	for _, r := range a.Records {
		if c, ok := r.Body.(*dnsmessage.CNAMEResource); ok {
			target = strings.TrimSuffix(c.CNAME.String(), ".")
		}
	}
	return target
}

// Takeovers: имена домена, которые можно захватить. Быстрый путь для аудита: имена из кэша логов
// сертификатов и перебора, без crt.sh в сети, и только те, чей CNAME ведёт на известный сервис.
func Takeovers(domain string) []Host {
	names := map[string]bool{}
	if c, ok := readCache(domain); ok {
		for _, n := range c.Value.Subs {
			names[n] = true
		}
	}
	for _, p := range Common {
		names[p+"."+domain] = true
	}
	all := slices.Sorted(maps.Keys(names))
	cnames := par.Map(all, 24, CNAME)
	var hosts []Host
	for i, target := range cnames {
		if target == "" || !slices.ContainsFunc(takeoverSigns, func(s takeoverSign) bool { return s.matches(target) }) {
			continue
		}
		hosts = append(hosts, Host{Name: all[i], CNAME: target, Source: "cname"})
	}
	hosts = par.Map(hosts, 8, func(h Host) Host {
		h.IPs = lookup(h.Name)
		probe(&h, false)
		return h
	})
	return slices.DeleteFunc(hosts, func(h Host) bool { return h.Takeover == nil })
}
