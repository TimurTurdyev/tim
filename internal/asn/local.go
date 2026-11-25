package asn

import (
	"sync"
	"time"

	"github.com/TimurTurdyev/tim/internal/netbase"
	"github.com/TimurTurdyev/tim/internal/paths"
)

// localFresh: таблицы старше этого не годятся - сети переезжают, и вместо таблиц спрашивается сеть.
const localFresh = 30 * 24 * time.Hour

var anyTables = sync.OnceValue(func() *netbase.Tables {
	t, err := netbase.Open(paths.Data("nets"))
	if err != nil {
		return nil
	}
	return t
})

// localTables: таблицы, которым ещё можно верить вместо сети.
func localTables() *netbase.Tables {
	if t := anyTables(); t != nil && time.Since(t.Built()) <= localFresh {
		return t
	}
	return nil
}

// LocalBuilt: когда собраны локальные таблицы сетей; fresh=false - они устарели, нулевое время - их нет.
func LocalBuilt() (built time.Time, fresh bool) {
	t := anyTables()
	if t == nil {
		return time.Time{}, false
	}
	return t.Built(), time.Since(t.Built()) <= localFresh
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

// Local: чьи адреса - только по локальным таблицам, даже устаревшим: наружу не уходит ничего. nil - таблиц нет.
func Local(ips []string) map[string]ASInfo {
	return lookup(anyTables(), ips)
}

// local: сети адресов из свежих локальных таблиц; nil - таблиц нет. Адреса, которого никто не объявляет, в ответе нет.
func local(ips []string) map[string]ASInfo {
	return lookup(localTables(), ips)
}

func lookup(t *netbase.Tables, ips []string) map[string]ASInfo {
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
