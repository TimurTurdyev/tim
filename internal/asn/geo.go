package asn

import (
	"encoding/json"
	"errors"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"github.com/TimurTurdyev/tim/internal/cache"
	"github.com/TimurTurdyev/tim/internal/paths"
)

// Nowhere: у сети нет места - так RIPEstat отвечает про адреса, которые объявлены сразу из многих стран.
const Nowhere = "?"

const geoTTL = 30 * 24 * time.Hour

// Place: место и сети, которые там стоят.
type Place struct {
	Country string         `json:"country"`
	City    string         `json:"city,omitempty"`
	Nets    []netip.Prefix `json:"nets"`
}

// Places: где стоят части сети - по MaxMind через RIPEstat; кэш месяц.
func Places(prefix string) ([]Place, error) {
	return cache.Get(cache.Path(paths.State("geo"), prefix), geoTTL, func() ([]Place, error) {
		if wait := retryIn(anyAS); wait > 0 {
			return nil, errors.New("RIPEstat не отвечал, повтор через " + wait.Round(time.Minute).String())
		}
		data, err := getJSON("https://stat.ripe.net/data/maxmind-geo-lite/data.json?resource=" + url.QueryEscape(prefix))
		if err != nil {
			if _, unreachable := errors.AsType[*url.Error](err); unreachable {
				failed(anyAS)
			}
			return nil, err
		}
		return ParseGeo(data)
	})
}

// ParseGeo разбирает maxmind-geo-lite: места сети и вложенные сети каждого места.
func ParseGeo(data []byte) ([]Place, error) {
	var doc struct {
		Status string `json:"status"`
		Data   struct {
			Located []struct {
				Locations []struct {
					Country   string   `json:"country"`
					City      string   `json:"city"`
					Resources []string `json:"resources"`
				} `json:"locations"`
			} `json:"located_resources"`
		} `json:"data"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	if doc.Status != "" && doc.Status != "ok" {
		return nil, errors.New("RIPEstat: " + doc.Status)
	}
	out := []Place{}
	for _, l := range doc.Data.Located {
		for _, loc := range l.Locations {
			p := Place{Country: strings.ToUpper(loc.Country), City: loc.City}
			for _, r := range loc.Resources {
				if net, err := netip.ParsePrefix(r); err == nil {
					p.Nets = append(p.Nets, net.Masked())
				}
			}
			out = append(out, p)
		}
	}
	return out, nil
}

// PlaceOf: место адреса - по самой узкой из вложенных сетей, куда он входит; пустое - неизвестно.
func PlaceOf(places []Place, ip string) Place {
	a, err := netip.ParseAddr(ip)
	if err != nil {
		return Place{}
	}
	var best Place
	bits := -1
	for _, p := range places {
		for _, net := range p.Nets {
			if net.Contains(a) && net.Bits() > bits {
				best, bits = Place{Country: p.Country, City: p.City}, net.Bits()
			}
		}
	}
	return best
}
