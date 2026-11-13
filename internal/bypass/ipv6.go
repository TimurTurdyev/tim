package bypass

import (
	"net/netip"
	"strconv"
	"strings"

	"github.com/TimurTurdyev/tim/internal/asn"
	"github.com/TimurTurdyev/tim/internal/config"
	"github.com/TimurTurdyev/tim/internal/netinfo"
	"github.com/TimurTurdyev/tim/internal/par"
	"github.com/TimurTurdyev/tim/internal/privileged"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// Обход ведёт цели по IPv4. Если у туннеля есть IPv6, а у домашней сети нет, программы идут к цели по IPv6
// через VPN, поэтому IPv6 целей закрывается: программа сразу получает отказ и переходит на IPv4.

// expand6: сети и адреса IPv6 цели; у адресов и подсетей IPv4 их нет.
func expand6(t netinfo.Target) []string {
	switch t.Kind {
	case netinfo.KindDomain:
		return netinfo.LookupIPv6(t.Value)
	case netinfo.KindAS:
		// Отказ RIPEstat уже показал Expand для IPv4, второй раз о нём не говорим.
		nets, _ := asn.Prefixes6(StateDir(), strings.TrimPrefix(t.Value, "as:"))
		return nets
	}
	return nil
}

func expandHosts6(hosts []config.Host) [][]string {
	return par.Map(hosts, par.Limit, func(h config.Host) []string {
		t, err := netinfo.ParseTarget(h.Addr)
		if err != nil {
			return nil
		}
		return expand6(t)
	})
}

// collect6: IPv6 тех же целей, что у CollectDests; без аргументов - всех целей конфига.
func collect6(cfg *config.Config, args []string) []string {
	if len(args) == 0 {
		return flatten(expandHosts6(cfg.Hosts))
	}
	var out []string
	for _, a := range args {
		if addr, ok := cfg.ResolveHost(a); ok {
			a = addr
		}
		if t, err := netinfo.ParseTarget(a); err == nil {
			out = append(out, expand6(t)...)
		}
	}
	return out
}

func prefixes6(dests []string) []netip.Prefix {
	seen := map[netip.Prefix]bool{}
	var out []netip.Prefix
	for _, d := range dests {
		if p, ok := netinfo.ParseV6(d); ok && !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	return out
}

// close6: закрыть ещё открытые сети; сколько закрыто сейчас, сколько уже было и что не вышло.
func close6(dests []string) (done, had int, failed []string) {
	have := netinfo.Rejects()
	for _, p := range prefixes6(dests) {
		if have[p] {
			had++
			continue
		}
		if _, err := privileged.Run(netinfo.RejectCmd(p)...); err != nil {
			failed = append(failed, p.String()+": "+err.Error())
			continue
		}
		done++
	}
	return done, had, failed
}

// state6: сколько сетей IPv6 целей закрыто из скольких; total 0 - закрывать нечего.
func state6(cfg *config.Config) (closed, total int) {
	have := netinfo.Rejects()
	ps := prefixes6(flatten(expandHosts6(cfg.Hosts)))
	for _, p := range ps {
		if have[p] {
			closed++
		}
	}
	return closed, len(ps)
}

// upTargets: маршруты к новым целям и закрытие IPv6 у всех целей конфига - новые в нём уже записаны.
func upTargets(cfg *config.Config, dests []string) error {
	if err := Up(cfg, dests); err != nil {
		return err
	}
	return Close6(collect6(cfg, nil))
}

// Close6: закрыть IPv6 целей и сказать, сколько закрыто.
func Close6(dests []string) error {
	done, had, failed := close6(dests)
	switch {
	case done > 0:
		line := ui.Label("IPv6 закрыт: "+ui.Count(done, "сеть", "сети", "сетей")) + ui.Dim(" - программы пойдут к целям по IPv4, в обход")
		if had > 0 {
			line += ui.Dim(", уже были " + strconv.Itoa(had))
		}
		ui.Println(line)
	case had > 0:
		ui.Println(ui.Dim("IPv6 уже закрыт: " + ui.Count(had, "сеть", "сети", "сетей")))
	}
	for _, f := range failed[:min(fewDests, len(failed))] {
		ui.Println(ui.Bad("  IPv6 " + f))
	}
	if len(failed) > 0 {
		return ui.Errorf("IPv6 не закрылся: %d из %d", len(failed), len(prefixes6(dests)))
	}
	return nil
}

// Open6: снять закрытие IPv6 у целей, которые убраны из обхода.
func Open6(dests []string) error {
	have := netinfo.Rejects()
	gone := 0
	var failed []string
	for _, p := range prefixes6(dests) {
		if !have[p] {
			continue
		}
		if _, err := privileged.Run(netinfo.UnrejectCmd(p)...); err != nil {
			failed = append(failed, p.String()+": "+err.Error())
			continue
		}
		gone++
	}
	if gone > 0 {
		ui.Println(ui.Dim("IPv6 открыт снова: " + ui.Count(gone, "сеть", "сети", "сетей")))
	}
	if len(failed) > 0 {
		return ui.Errorf("IPv6 не открылся: %d, первый %s", len(failed), failed[0])
	}
	return nil
}
