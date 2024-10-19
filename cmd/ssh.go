package cmd

import (
	"strings"

	"github.com/spf13/cobra"

	"github.com/TimurTurdyev/tim/internal/netinfo"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/sshx"
	"github.com/TimurTurdyev/tim/internal/tui"
	"github.com/TimurTurdyev/tim/internal/ui"
)

var sshCmd = &cobra.Command{
	Use:   "ssh [алиас|проект|user@host] [аргументы ssh]",
	Short: "ssh с обходом VPN: если адрес уходит в туннель, предложит маршрут",
	Long: `Цель может быть алиасом из ~/.ssh/config, именем проекта из конфига или user@host.
Перед подключением tim смотрит, куда уходит адрес. Если в туннель VPN или на шлюз
прежней сети, предлагает завести host-маршрут через физический шлюз и только потом
подключается. Без аргументов открывает выбор алиаса, ssh list печатает их списком.`,
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) > 0 {
			return nil, cobra.ShellCompDirectiveDefault
		}
		names, _ := projectNames(cmd, args, toComplete)
		return append(sshx.Aliases(), names...), cobra.ShellCompDirectiveNoFileComp
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) > 0 && args[0] == "list" || len(args) == 0 && !tui.Available() {
			return sshList()
		}
		if len(args) == 0 {
			alias, err := pickAlias()
			if err != nil {
				return err
			}
			args = []string{alias}
		}
		target, err := sshTarget(args[0])
		if err != nil {
			return err
		}
		if ip := sshx.IP(target); ip != "" {
			ensureBypass(ip)
		}
		return runner.Exec("ssh", append([]string{target}, args[1:]...)...)
	},
}

// sshTarget: алиас как есть, проект через его поле ssh, иначе user@host или host.
func sshTarget(want string) (string, error) {
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

// ensureBypass предлагает маршрут, если адрес не доедет напрямую.
func ensureBypass(ip string) {
	if netinfo.IsPrivate(ip) {
		return
	}
	phys, gw := physical()
	state, bad := routeState(ip, phys, gw)
	if !bad {
		return
	}
	ui.Println(ip + ": " + state)
	if runner.Confirm("завести маршрут через " + gw + "?") {
		_ = routeUp([]string{ip})
	}
}

func sshList() error {
	for _, a := range sshx.Aliases() {
		if a == "github.com" || strings.HasPrefix(a, "git.") {
			continue
		}
		t := sshx.Resolve(a)
		ui.Println(ui.Label(ui.Pad(a, 24)) + " " + ui.Pad(t.User+"@"+t.Host, 34) + " " + ui.Dim(strings.Join(projectsBySSH(a), " ")))
	}
	return nil
}

func init() { root.AddCommand(sshCmd) }
