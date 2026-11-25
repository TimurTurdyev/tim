package sites

import (
	"slices"
	"testing"
	"time"

	"github.com/TimurTurdyev/tim/internal/baseline"
	"github.com/TimurTurdyev/tim/internal/domaudit"
	"github.com/TimurTurdyev/tim/internal/internetdb"
	"github.com/TimurTurdyev/tim/internal/portscan"
)

// Тревога - новое имя знакомого источника и новый порт знакомого адреса. Первый ответ источника, урезанный список и
// впервые увиденный адрес показывают старое, о котором мы не знали.
func TestOutside(t *testing.T) {
	t.Setenv("TIM_STATE_DIR", t.TempDir())
	now := time.Date(2026, 3, 10, 3, 0, 0, 0, time.UTC)
	host := func(name string, sources ...string) domaudit.Host {
		return domaudit.Host{Name: name, IPs: []string{"192.0.2.1"}, Source: "THC", Sources: sources}
	}
	addr := func(ip string, ports ...int) domaudit.Addr {
		return domaudit.Addr{IP: ip, Own: true, Scanned: true, Services: &internetdb.Info{Ports: ports}}
	}
	checked := func(ip string, ports ...int) domaudit.Addr {
		a := domaudit.Addr{IP: ip, Own: true, Checked: true}
		for _, p := range ports {
			a.Open = append(a.Open, portscan.Service{Port: p, Name: "http"})
		}
		return a
	}
	source := func(name string, cut bool) domaudit.SourceState {
		return domaudit.SourceState{Name: name, Names: 1, Cut: cut}
	}
	steps := []struct {
		name string
		rep  domaudit.Report
		want []string // что ждёт отметки
	}{
		{"первое наблюдение запоминает всё", domaudit.Report{Hosts: []domaudit.Host{host("a.example.com", "THC")},
			Sources: []domaudit.SourceState{source("THC", false)}, Addrs: []domaudit.Addr{addr("192.0.2.1", 80, 443)}}, nil},
		{"первый ответ crt.sh - не тревога", domaudit.Report{
			Hosts:   []domaudit.Host{host("a.example.com", "THC", "crt.sh"), host("old.example.com", "crt.sh")},
			Sources: []domaudit.SourceState{source("THC", false), source("crt.sh", false)}, Addrs: []domaudit.Addr{addr("192.0.2.1", 80, 443)}}, nil},
		{"новое имя того же crt.sh - тревога", domaudit.Report{
			Hosts:   []domaudit.Host{host("a.example.com", "THC"), host("old.example.com", "crt.sh"), host("vpn.example.com", "crt.sh")},
			Sources: []domaudit.SourceState{source("THC", false), source("crt.sh", false)}, Addrs: []domaudit.Addr{addr("192.0.2.1", 80, 443)}},
			[]string{"host vpn.example.com"}},
		{"имя урезанного списка и порты нового адреса - не тревога, порт знакомого - тревога", domaudit.Report{
			Hosts:   []domaudit.Host{host("a.example.com", "THC"), host("vpn.example.com", "crt.sh"), host("cut.example.com", "THC")},
			Sources: []domaudit.SourceState{source("THC", true), source("crt.sh", false)},
			Addrs:   []domaudit.Addr{addr("192.0.2.1", 80, 443, 8080), addr("192.0.2.7", 22, 443)}},
			[]string{"host vpn.example.com", "port 192.0.2.1:8080"}},
		{"DNS не ответил - ничего не пропало", domaudit.Report{}, []string{"host vpn.example.com", "port 192.0.2.1:8080"}},
		{"про адрес сегодня не спрашивали - его порт ждёт", domaudit.Report{
			Hosts:   []domaudit.Host{host("a.example.com", "THC"), host("vpn.example.com", "crt.sh")},
			Sources: []domaudit.SourceState{source("THC", false), source("crt.sh", false)},
			Addrs:   []domaudit.Addr{{IP: "192.0.2.1", Own: true}, addr("192.0.2.7", 22, 443)}},
			[]string{"host vpn.example.com", "port 192.0.2.1:8080"}},
		{"порт нового адреса в следующий раз - тревога, закрытый порт уходит сам", domaudit.Report{
			Hosts:   []domaudit.Host{host("a.example.com", "THC"), host("vpn.example.com", "crt.sh")},
			Sources: []domaudit.SourceState{source("THC", false), source("crt.sh", false)},
			Addrs:   []domaudit.Addr{addr("192.0.2.1", 80, 443), addr("192.0.2.7", 22, 443, 3306)}},
			[]string{"host vpn.example.com", "port 192.0.2.7:3306"}},
		{"порты проверили сами впервые - не тревога", domaudit.Report{
			Hosts:   []domaudit.Host{host("a.example.com", "THC"), host("vpn.example.com", "crt.sh")},
			Sources: []domaudit.SourceState{source("THC", false), source("crt.sh", false)},
			Addrs:   []domaudit.Addr{checked("192.0.2.1", 25, 80, 443), addr("192.0.2.7", 22, 443, 3306)}},
			[]string{"host vpn.example.com", "port 192.0.2.7:3306"}},
		{"новый порт в своей проверке - тревога, молчащий - нет", domaudit.Report{
			Hosts:   []domaudit.Host{host("a.example.com", "THC"), host("vpn.example.com", "crt.sh")},
			Sources: []domaudit.SourceState{source("THC", false), source("crt.sh", false)},
			Addrs: []domaudit.Addr{{IP: "192.0.2.1", Own: true, Checked: true, Open: []portscan.Service{{Port: 80}, {Port: 443}, {Port: 6379, Name: "redis"},
				{Port: 110, Silent: true}}}, addr("192.0.2.7", 22, 443, 3306)}},
			[]string{"host vpn.example.com", "port 192.0.2.1:6379", "port 192.0.2.7:3306"}},
	}
	for i, s := range steps {
		s.rep.Domain = "example.com"
		observeOutside(&s.rep, now.Add(time.Duration(i)*24*time.Hour))
		var got []string
		for _, it := range baseline.Pending(outsideObject("example.com")) {
			got = append(got, it.Kind+" "+it.Key)
		}
		slices.Sort(got)
		if !slices.Equal(got, s.want) {
			t.Errorf("%s: %v, ждали %v", s.name, got, s.want)
		}
	}
}

// Свой источник проверяет сколько успеет и меняет словарь: имя, до которого он дошёл впервые, - не тревога.
// Тревога - новое имя в полном проходе тем же словарём.
func TestOutsideOwnSource(t *testing.T) {
	t.Setenv("TIM_STATE_DIR", t.TempDir())
	now := time.Date(2026, 3, 10, 3, 0, 0, 0, time.UTC)
	host := func(name string, recalled bool) domaudit.Host {
		h := domaudit.Host{Name: name, IPs: []string{"192.0.2.1"}, Source: "словарь", Sources: []string{"словарь"}}
		if recalled {
			h.Recalled = h.Sources
		}
		return h
	}
	words := func(version string, cut bool) []domaudit.SourceState {
		return []domaudit.SourceState{{Name: "словарь", Own: true, Names: 1, Version: version, Cut: cut}}
	}
	steps := []struct {
		name     string
		hosts    []string
		state    []domaudit.SourceState
		want     []string
		recalled string // имя, которое сбор только помнит с прошлого раза
	}{
		{"первое наблюдение: словарь успел не всё", []string{"a"}, words("1", true), nil, ""},
		{"первый полный проход находит старое", []string{"a", "b"}, words("1", false), nil, ""},
		{"новое имя в полном проходе - тревога", []string{"a", "b", "c"}, words("1", false), []string{"host c.example.com"}, ""},
		{"урезанный проход не снимает тревогу и не поднимает новую", []string{"a", "b", "c", "d"}, words("1", true), []string{"host c.example.com"}, ""},
		{"новый словарь находит старое", []string{"a", "b", "c", "d", "e"}, words("2", false), []string{"host c.example.com"}, ""},
		{"новое имя новым словарём на другой день - тревога", []string{"a", "b", "c", "d", "e", "f"}, words("2", false),
			[]string{"host c.example.com", "host f.example.com"}, ""},
		{"имя из урезанного прохода своим не становится", []string{"a", "b", "c", "d", "e", "f", "g"}, words("2", true),
			[]string{"host c.example.com", "host f.example.com"}, ""},
		{"и полный проход поднимает его как новое", []string{"a", "b", "c", "d", "e", "f", "g"}, words("2", false),
			[]string{"host c.example.com", "host f.example.com", "host g.example.com"}, ""},
		{"имя, которое сбор только помнит, не новое и не своё", []string{"a", "b", "c", "d", "e", "f", "g", "h"}, words("2", false),
			[]string{"host c.example.com", "host f.example.com", "host g.example.com"}, "h"},
		{"найдено заново полным проходом - тревога", []string{"a", "b", "c", "d", "e", "f", "g", "h"}, words("2", false),
			[]string{"host c.example.com", "host f.example.com", "host g.example.com", "host h.example.com"}, ""},
	}
	for i, s := range steps {
		rep := domaudit.Report{Domain: "example.com", Sources: s.state}
		for _, h := range s.hosts {
			rep.Hosts = append(rep.Hosts, host(h+".example.com", h == s.recalled))
		}
		observeOutside(&rep, now.Add(time.Duration(i)*24*time.Hour))
		var got []string
		for _, it := range baseline.Pending(outsideObject("example.com")) {
			got = append(got, it.Kind+" "+it.Key)
		}
		slices.Sort(got)
		if !slices.Equal(got, s.want) {
			t.Errorf("%s: %v, ждали %v", s.name, got, s.want)
		}
	}
}
