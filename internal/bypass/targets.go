// Package bypass: обход VPN - цели из конфига, маршруты через физический шлюз, страж, наблюдение
// за туннелем и обход по приложениям.
package bypass

import (
	"strings"

	"github.com/TimurTurdyev/tim/internal/asn"
	"github.com/TimurTurdyev/tim/internal/config"
	"github.com/TimurTurdyev/tim/internal/netinfo"
	"github.com/TimurTurdyev/tim/internal/paths"
	"github.com/TimurTurdyev/tim/internal/ui"
)

func Expand(t netinfo.Target) []string {
	switch t.Kind {
	case netinfo.KindDomain:
		return netinfo.LookupIPv4(t.Value)
	case netinfo.KindAS:
		nets, err := asn.Prefixes(StateDir(), strings.TrimPrefix(t.Value, "as:"))
		if err != nil {
			ui.Println(ui.Warn(t.Value + ": сети не получены: " + ui.ShortErr(err)))
		}
		return nets
	}
	return []string{t.Value}
}

// StateDir: служебные файлы. Страж работает под root, и в домашней папке он наплодил бы файлов,
// которые пользователь потом не перезапишет, поэтому у root своё место.
func StateDir() string { return paths.OwnState() }

// OwnerTargets: домен -> автономные системы, из которых видны его адреса. Их часто несколько:
// Яндекс анонсирует одни и те же сети и из основной AS, и из дочерней. Берём все - пересечения
// склеятся при раскрытии.
func OwnerTargets(domain string, force bool) ([]netinfo.Target, []string, error) {
	ips := netinfo.LookupIPv4(domain)
	if len(ips) == 0 {
		return nil, nil, ui.Errorf("%s не раскрывается в адреса", domain)
	}
	list, err := asn.Origins(ips)
	if err != nil || len(list) == 0 {
		return nil, nil, ui.Errorf("%s: владелец сети не определился: %v", domain, err)
	}
	var targets []netinfo.Target
	var names []string
	for _, a := range list {
		name := asn.Holder(a)
		if asn.IsCDN(name) && !force {
			return nil, nil, ui.Errorf("%s сидит на CDN (%s): в обход ушла бы чужая половина интернета; если точно надо: --force", domain, name)
		}
		targets = append(targets, netinfo.Target{Kind: netinfo.KindAS, Value: "as:" + a})
		names = append(names, name)
	}
	return targets, names, nil
}

func ExpandAddr(addr string) []string {
	t, err := netinfo.ParseTarget(addr)
	if err != nil {
		return nil
	}
	return Expand(t)
}

// CollectDests: аргументы как пометки, цели из конфига или новые цели; без аргументов все цели конфига.
func CollectDests(cfg *config.Config, args []string) ([]string, error) {
	if len(args) == 0 {
		var all []string
		for _, h := range cfg.Hosts {
			all = append(all, ExpandAddr(h.Addr)...)
		}
		return all, nil
	}
	var out []string
	for _, a := range args {
		if addr, ok := cfg.ResolveHost(a); ok {
			out = append(out, ExpandAddr(addr)...)
			continue
		}
		t, err := netinfo.ParseTarget(a)
		if err != nil {
			return nil, err
		}
		ips := Expand(t)
		if len(ips) == 0 {
			return nil, ui.Errorf("%s не раскрывается в адреса", a)
		}
		out = append(out, ips...)
	}
	return out, nil
}
