package cmd

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"

	"github.com/TimurTurdyev/tim/internal/brew"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/ui"
)

var resolverOff bool

// dnsmasqDir: куда этот дистрибутив или brew кладут куски конфига dnsmasq.
func dnsmasqDir() string {
	if runtime.GOOS == "darwin" {
		return filepath.Join(brew.Prefix(), "etc", "dnsmasq.d")
	}
	return "/etc/dnsmasq.d"
}

func resolverPath(tld string) string { return filepath.Join("/etc/resolver", tld) }
func dnsmasqPath(tld string) string  { return filepath.Join(dnsmasqDir(), "tim-"+tld+".conf") }

// restartDnsmasq: dnsmasq слушает 53-й порт, поэтому запускается от root.
func restartDnsmasq() error {
	if runtime.GOOS == "darwin" {
		return runner.Sudo("brew", "services", "restart", "dnsmasq")
	}
	if runner.Has("systemctl") {
		return runner.Sudo("systemctl", "restart", "dnsmasq")
	}
	return ui.Errorf("перезапусти dnsmasq сам: как - зависит от системы")
}

var hostResolverCmd = &cobra.Command{
	Use:   "resolver [домен верхнего уровня] [--off]",
	Short: "Весь домен вида *.test на 127.0.0.1 через dnsmasq, без записей в /etc/hosts",
	Long: `/etc/hosts знает только точные имена, поэтому поддомены вида a.shop.test в него не записать.
Резолвер решает это целиком: dnsmasq отдаёт 127.0.0.1 на любое имя в зоне, а /etc/resolver/<зона>
говорит системе спрашивать про эту зону у него. Без аргумента показывает уже настроенные зоны.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return listResolvers()
		}
		tld := strings.TrimPrefix(strings.TrimSuffix(args[0], "."), ".")
		if tld == "" || strings.Contains(tld, "/") {
			return ui.Errorf("нужна зона целиком, например test или localhost")
		}
		if resolverOff {
			if err := runner.RemoveRoot(resolverPath(tld)); err != nil {
				return err
			}
			if err := runner.RemoveRoot(dnsmasqPath(tld)); err != nil {
				return err
			}
			ui.Println(ui.Label("выключено") + ui.Dim("  зона "+tld))
			return restartDnsmasq()
		}
		if runtime.GOOS != "darwin" {
			// /etc/resolver - механизм macOS, на Linux его никто не читает.
			return ui.Errorf("на Linux зону целиком так не настроить: имена *.localhost systemd-resolved и так отдаёт 127.0.0.1, точные имена - tim host add")
		}
		if !runner.Has("dnsmasq") {
			hint := "brew install dnsmasq"
			if runtime.GOOS != "darwin" {
				hint = "поставь dnsmasq пакетным менеджером системы"
			}
			return ui.Errorf("нет dnsmasq, без него зону целиком не отдать: %s", hint)
		}
		ui.Println(ui.Dim("любое имя в зоне ." + tld + " будет отвечать 127.0.0.1"))
		if !runner.Confirm("настроить зону " + tld + "?") {
			return ui.Errorf("отменено")
		}
		if err := runner.WriteRoot(dnsmasqPath(tld), []byte("address=/."+tld+"/127.0.0.1\n")); err != nil {
			return err
		}
		if err := runner.WriteRoot(resolverPath(tld), []byte("nameserver 127.0.0.1\n")); err != nil {
			return err
		}
		if err := restartDnsmasq(); err != nil {
			return err
		}
		ui.Println(ui.Label("готово") + ui.Dim("  проверить: tim host check что-угодно."+tld))
		return nil
	},
}

func listResolvers() error {
	files, _ := filepath.Glob("/etc/resolver/*")
	if len(files) == 0 {
		ui.Println(ui.Dim("настроенных зон нет: tim host resolver test"))
		return nil
	}
	for _, f := range files {
		zone := filepath.Base(f)
		line := ui.Pad(zone, 20)
		data, err := os.ReadFile(f)
		if err != nil {
			line += ui.Dim("не прочитать")
		} else {
			for _, l := range strings.Split(string(data), "\n") {
				if ns, ok := strings.CutPrefix(strings.TrimSpace(l), "nameserver "); ok {
					line += ns + " "
				}
			}
		}
		if _, err := os.Stat(dnsmasqPath(zone)); err == nil {
			line += ui.Dim(" настроено через tim")
		}
		ui.Println(line)
	}
	return nil
}

func init() {
	hostResolverCmd.Flags().BoolVar(&resolverOff, "off", false, "убрать зону")
	hostCmd.AddCommand(hostResolverCmd)
}
