package local

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/TimurTurdyev/tim/internal/brew"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// setResolver: зона целиком на 127.0.0.1 - dnsmasq отвечает, /etc/resolver/<зона> отправляет к нему.
func setResolver(zone string, off bool) error {
	tld := strings.TrimPrefix(strings.TrimSuffix(zone, "."), ".")
	if tld == "" || strings.Contains(tld, "/") {
		return ui.Errorf("нужна зона целиком, например test или localhost")
	}
	if off {
		return dropResolver(tld)
	}
	if runtime.GOOS != "darwin" {
		// /etc/resolver - механизм macOS, на Linux его никто не читает.
		return ui.Errorf("на Linux зону целиком так не настроить: имена *.localhost systemd-resolved и так отдаёт 127.0.0.1, точные имена - tim host add")
	}
	if !runner.Has("dnsmasq") {
		return ui.Errorf("нет dnsmasq, без него зону целиком не отдать: brew install dnsmasq")
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
}

func dropResolver(tld string) error {
	if err := runner.RemoveRoot(resolverPath(tld)); err != nil {
		return err
	}
	if err := runner.RemoveRoot(dnsmasqPath(tld)); err != nil {
		return err
	}
	ui.Println(ui.Label("выключено") + ui.Dim("  зона "+tld))
	return restartDnsmasq()
}

func listResolvers() {
	files, _ := filepath.Glob("/etc/resolver/*")
	if len(files) == 0 {
		ui.Println(ui.Dim("настроенных зон нет: tim host resolver test"))
		return
	}
	for _, f := range files {
		zone := filepath.Base(f)
		line := ui.Pad(zone, 20)
		if data, err := os.ReadFile(f); err != nil {
			line += ui.Dim("не прочитать")
		} else {
			line += strings.Join(nameservers(string(data)), " ") + " "
		}
		if _, err := os.Stat(dnsmasqPath(zone)); err == nil {
			line += ui.Dim(" настроено через tim")
		}
		ui.Println(line)
	}
}

// nameservers: адреса из файла /etc/resolver/<зона>.
func nameservers(text string) []string {
	var out []string
	for _, l := range strings.Split(text, "\n") {
		if ns, ok := strings.CutPrefix(strings.TrimSpace(l), "nameserver "); ok {
			out = append(out, ns)
		}
	}
	return out
}

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
