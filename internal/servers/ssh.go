package servers

import (
	"strings"

	"github.com/TimurTurdyev/tim/internal/app"
	"github.com/TimurTurdyev/tim/internal/bypass"
	"github.com/TimurTurdyev/tim/internal/config"
	"github.com/TimurTurdyev/tim/internal/netinfo"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/sshx"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// ssh: алиас, проект или user@host; без аргумента - выбор алиаса, list - список.
func ssh(env *app.Env, args []string) error {
	if len(args) > 0 && args[0] == "list" || len(args) == 0 && !app.Interactive() {
		printAliases(env)
		return nil
	}
	if len(args) == 0 {
		alias, err := env.PickAlias()
		if err != nil {
			return err
		}
		args = []string{alias}
	}
	target, err := sshTarget(env.Cfg, args[0])
	if err != nil {
		return err
	}
	ensureBypass(env.Cfg, target)
	return runner.Exec("ssh", append([]string{target}, args[1:]...)...)
}

// Target: куда заходить отчёту о сервере - ssh проекта или аргумент как есть.
func Target(cfg *config.Config, arg string) string {
	if p, ok := cfg.Project(arg); ok && p.SSH != "" {
		return p.SSH
	}
	return arg
}

// sshTarget: алиас как есть, проект через его поле ssh, иначе user@host или host.
func sshTarget(cfg *config.Config, want string) (string, error) {
	if sshx.IsAlias(want) || strings.Contains(want, "@") {
		return want, nil
	}
	if p, ok := cfg.Project(want); ok {
		if p.SSH == "" {
			return "", ui.Errorf("у проекта %s нет ssh-алиаса в конфиге", want)
		}
		return p.SSH, nil
	}
	if netinfo.IsIPv4(want) || strings.Contains(want, ".") {
		return want, nil
	}
	return "", ui.Errorf("нет алиаса %s в ~/.ssh/config и проекта с таким именем", want)
}

// ensureBypass: если адрес сервера уходит в туннель или на шлюз прежней сети, предлагает маршрут
// через физический шлюз - иначе ssh повиснет на VPN.
func ensureBypass(cfg *config.Config, target string) {
	ip := sshx.IP(target)
	if ip == "" || netinfo.IsPrivate(ip) {
		return
	}
	phys, gw := netinfo.Physical()
	state, bad := bypass.RouteState(ip, phys, gw)
	if !bad {
		return
	}
	ui.Println(ip + ": " + state)
	if runner.Confirm("завести маршрут через " + gw + "?") {
		_ = bypass.Up(cfg, []string{ip})
	}
}

func printAliases(env *app.Env) {
	for _, a := range sshx.Aliases() {
		if a == "github.com" || strings.HasPrefix(a, "git.") {
			continue
		}
		t := sshx.Resolve(a)
		ui.Println(ui.Label(ui.Pad(a, 24)) + " " + ui.Pad(t.User+"@"+t.Host, 34) + " " + ui.Dim(strings.Join(env.ProjectsOn(a), " ")))
	}
}
