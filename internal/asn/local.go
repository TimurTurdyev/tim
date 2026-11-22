package asn

import (
	"sync"
	"time"

	"github.com/TimurTurdyev/tim/internal/netbase"
	"github.com/TimurTurdyev/tim/internal/paths"
)

// localFresh: таблицы старше этого не годятся - сети переезжают, и вместо таблиц спрашивается сеть.
const localFresh = 30 * 24 * time.Hour

var localTables = sync.OnceValue(func() *netbase.Tables {
	t, err := netbase.Open(paths.Data("nets"))
	if err != nil || time.Since(t.Built()) > localFresh {
		return nil
	}
	return t
})

// LocalBuilt: когда собраны локальные таблицы сетей; ok=false - их нет или они устарели.
func LocalBuilt() (time.Time, bool) {
	t := localTables()
	if t == nil {
		return time.Time{}, false
	}
	return t.Built(), true
}

// Where: в какой стране стоит сервер - по локальным таблицам; ok=false - таблиц нет или адреса в них нет.
func Where(ip string) (country string, ok bool) {
	t := localTables()
	if t == nil {
		return "", false
	}
	info, _ := t.Lookup(ip)
	return info.Country, info.Country != ""
}

// local: сети адресов из локальных таблиц; nil - таблиц нет. Адреса, которого никто не объявляет, в ответе нет.
func local(ips []string) map[string]ASInfo {
	t := localTables()
	if t == nil {
		return nil
	}
	out := map[string]ASInfo{}
	for _, ip := range ips {
		if info, ok := t.Lookup(ip); ok && info.ASN != "" {
			out[ip] = ASInfo{ASN: info.ASN, Prefix: info.Prefix, Country: info.Registered, Name: info.Name}
		}
	}
	return out
}
