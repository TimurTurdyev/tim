package bypass

import (
	"strings"

	"github.com/TimurTurdyev/tim/internal/app"
	"github.com/TimurTurdyev/tim/internal/config"
	"github.com/TimurTurdyev/tim/internal/netinfo"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// addTargets: цели в конфиг и маршруты к ним; с owner домен заменяется сетями его владельца.
func addTargets(env *app.Env, args []string, o addOptions) error {
	var dests []string
	for _, a := range args {
		targets, label, err := resolveTarget(a, o, len(args) == 1)
		if err != nil {
			return err
		}
		for _, t := range targets {
			if netinfo.PrivateTarget(t) && !o.force {
				return ui.Errorf("%s - частная сеть, она обычно доступна только через VPN; если точно надо: --force", t.Value)
			}
			if env.Cfg.AddHost(config.Host{Addr: t.Value, Label: label}) {
				ui.Println(ui.Label("добавлен ") + t.Value + ui.Dim(" "+label))
			} else {
				ui.Println(ui.Dim(t.Value + ": уже в списке"))
			}
			dests = append(dests, Expand(t)...)
		}
	}
	if err := env.Save(); err != nil {
		return err
	}
	if o.noUp {
		return nil
	}
	return Up(env.Cfg, dests)
}

// resolveTarget: цель из аргумента; домен с owner - сети владельца, и пометкой становится сам домен.
// Пометка ставится, только когда цель одна: на список одной пометки не напасёшься.
func resolveTarget(arg string, o addOptions, single bool) ([]netinfo.Target, string, error) {
	t, err := netinfo.ParseTarget(arg)
	if err != nil {
		return nil, "", err
	}
	label := ""
	if single {
		label = o.label
	}
	if !o.owner || t.Kind != netinfo.KindDomain {
		return []netinfo.Target{t}, label, nil
	}
	targets, names, err := OwnerTargets(t.Value, o.force)
	if err != nil {
		return nil, "", err
	}
	if label == "" && single {
		label = t.Value
	}
	for i, n := range targets {
		ui.Println(ui.Dim(t.Value + ": сети " + n.Value + " " + names[i]))
	}
	return targets, label, nil
}

// removeTargets: цели по адресу, домену или пометке - из конфига, их маршруты - из таблицы.
func removeTargets(env *app.Env, args []string) error {
	var dests []string
	for _, a := range args {
		removed := env.Cfg.RemoveHost(a)
		if len(removed) == 0 {
			if t, err := netinfo.ParseTarget(a); err == nil {
				removed = env.Cfg.RemoveHost(t.Value)
			}
		}
		if len(removed) == 0 {
			return ui.Errorf("в списке нет %s: tim route list", a)
		}
		for _, h := range removed {
			ui.Println(ui.Label("убран ") + h.Addr)
		}
		dests = append(dests, flatten(ExpandHosts(removed))...)
	}
	if err := env.Save(); err != nil {
		return err
	}
	return Down(dests)
}

func up(env *app.Env, args []string) error {
	dests, err := CollectDests(env.Cfg, args)
	if err != nil {
		return err
	}
	return Up(env.Cfg, dests)
}

func down(env *app.Env, args []string) error {
	dests, err := CollectDests(env.Cfg, args)
	if err != nil {
		return err
	}
	return Down(dests)
}

// fix: после смены сети - снять протухшие маршруты и завести все цели заново.
func fix(env *app.Env) error {
	if err := Clean(env.Cfg, false); err != nil {
		return err
	}
	return up(env, nil)
}

func printTargets(cfg *config.Config) {
	if len(cfg.Hosts) == 0 {
		ui.Println(ui.Dim("список пуст: tim route add <адрес|домен|подсеть>"))
	}
	for _, h := range cfg.Hosts {
		ui.Println(ui.Pad(h.Addr, 24) + " " + h.Label)
	}
}

func printTable(cfg *config.Config) {
	for _, r := range netinfo.Table() {
		if !strings.Contains(r.Flags, "S") || !strings.Contains(r.Flags, "G") {
			continue
		}
		ui.Println(ui.Pad(r.Target(), 20) + " " + ui.Pad(r.Gateway, 16) + " " + ui.Pad(r.Iface, 7) + " " + ui.Dim(cfg.HostLabel(r.Target())))
	}
}
