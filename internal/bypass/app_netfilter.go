package bypass

import (
	"errors"
	"os"
	"strconv"
	"strings"

	"github.com/TimurTurdyev/tim/internal/netinfo"
	"github.com/TimurTurdyev/tim/internal/privileged"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// Linux: ядро само умеет маршрутизировать по владельцу сокета. Пакеты процессов группы novpn
// получают метку (netfilter owner), по метке своя таблица маршрутов с шлюзом физического
// интерфейса, а источник подменяется на его адрес (MASQUERADE). Прокси не нужен.

const (
	linuxMark  = "0x7469" // "ti"
	linuxTable = "7788"
)

// linuxGID: группа через getent, при первом разе - groupadd.
func linuxGID() (int, error) {
	if gid, ok := linuxGroup(); ok {
		return gid, nil
	}
	if _, err := privileged.Run("groupadd", "--system", GroupName); err != nil {
		return 0, ui.Errorf("группа %s не завелась: %v", GroupName, err)
	}
	if runner.DryRun {
		return 0, nil
	}
	gid, ok := linuxGroup()
	if !ok {
		return 0, ui.Errorf("группа %s не появилась после groupadd", GroupName)
	}
	ui.Println(ui.Label("группа "+GroupName) + ui.Dim("  gid "+strconv.Itoa(gid)))
	return gid, nil
}

func linuxGroup() (int, bool) {
	out, err := runner.Output("getent", "group", GroupName)
	if err != nil {
		return 0, false
	}
	f := strings.Split(strings.TrimSpace(out), ":")
	if len(f) < 3 {
		return 0, false
	}
	gid, err := strconv.Atoi(f[2])
	return gid, err == nil
}

// linuxRules: правила netfilter для группы; -C проверяет, -A добавляет, -D снимает.
func linuxRules(gid int, phys string) [][]string {
	return [][]string{
		{"-t", "mangle", "OUTPUT", "-m", "owner", "--gid-owner", strconv.Itoa(gid), "-j", "MARK", "--set-mark", linuxMark},
		{"-t", "nat", "POSTROUTING", "-m", "mark", "--mark", linuxMark, "-o", phys, "-j", "MASQUERADE"},
	}
}

func iptables() (string, error) {
	for _, name := range []string{"iptables", "iptables-nft", "iptables-legacy"} {
		if p, err := runner.LookPath(name); err == nil {
			return p, nil
		}
	}
	return "", ui.Errorf("нужен iptables: apt install iptables (или dnf/pacman)")
}

func withAction(rule []string, action string) []string {
	// Цепочка - третий элемент: перед ней ставится -A, -C или -D.
	out := append([]string{}, rule[:2]...)
	out = append(out, action, rule[2])
	return append(out, rule[3:]...)
}

// linuxOn: метка, маскарад, правило и таблица, rp_filter посвободнее.
func linuxOn(gid int, phys netinfo.Service, gw string) error {
	ipt, err := iptables()
	if err != nil {
		return err
	}
	for _, rule := range linuxRules(gid, phys.Device) {
		if _, err := privileged.Run(append([]string{ipt}, withAction(rule, "-C")...)...); err == nil && !runner.DryRun {
			continue
		}
		if _, err := privileged.Run(append([]string{ipt}, withAction(rule, "-A")...)...); err != nil {
			return ui.Errorf("iptables не принял правило: %v", err)
		}
	}
	if rules, _ := runner.Output("ip", "rule", "show"); !strings.Contains(rules, "fwmark "+linuxMark+" lookup "+linuxTable) {
		if _, err := privileged.Run("ip", "rule", "add", "fwmark", linuxMark, "lookup", linuxTable); err != nil {
			return ui.Errorf("маршрут по метке не добавился: %v", err)
		}
	}
	if _, err := privileged.Run("ip", "route", "replace", "default", "via", gw, "dev", phys.Device, "table", linuxTable); err != nil {
		return ui.Errorf("маршрут через %s в таблице %s не добавился: %v", phys.Device, linuxTable, err)
	}
	// Ответы приходят на физический интерфейс, а обратный путь по основной таблице ведёт в туннель:
	// строгий rp_filter их выбросил бы.
	for _, key := range []string{"net.ipv4.conf." + phys.Device + ".rp_filter", "net.ipv4.conf.all.rp_filter"} {
		if cur := sysctlGet(key); cur == "1" {
			if _, err := privileged.Run("sysctl", "-w", key+"=2"); err == nil {
				remember(strings.ReplaceAll(key, ".", "_"), "1")
			}
		}
	}
	ui.Println(ui.Label("мимо VPN") + ui.Dim("  группа "+GroupName+", метка "+linuxMark+" -> таблица "+linuxTable+" через "+phys.Device+" "+gw))
	return nil
}

func sysctlGet(key string) string {
	data, err := os.ReadFile("/proc/sys/" + strings.ReplaceAll(key, ".", "/"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func linuxOff() error {
	ipt, err := iptables()
	if err != nil {
		return err
	}
	phys, _ := Physical()
	if gid, ok := linuxGroup(); ok {
		for _, rule := range linuxRules(gid, phys.Device) {
			// Снимаем, пока снимается: правило могло попасть в цепочку не один раз.
			for range 5 {
				if _, err := privileged.Run(append([]string{ipt}, withAction(rule, "-D")...)...); err != nil || runner.DryRun {
					break
				}
			}
		}
	}
	var errs []error
	if rules, _ := runner.Output("ip", "rule", "show"); strings.Contains(rules, "fwmark "+linuxMark+" lookup "+linuxTable) {
		if out, err := privileged.Run("ip", "rule", "del", "fwmark", linuxMark, "lookup", linuxTable); err != nil {
			errs = append(errs, ui.Errorf("маршрут по метке не снялся: %s", firstLine(out, err)))
		}
	}
	if out, err := privileged.Run("ip", "route", "flush", "table", linuxTable); err != nil {
		errs = append(errs, ui.Errorf("таблица %s не очистилась: %s", linuxTable, firstLine(out, err)))
	}
	keys := []string{"net.ipv4.conf.all.rp_filter"}
	if phys.Device != "" {
		keys = append(keys, "net.ipv4.conf."+phys.Device+".rp_filter")
	}
	for _, key := range keys {
		errs = append(errs, sysctlRestore(key))
	}
	if err := errors.Join(errs...); err != nil {
		return err
	}
	ui.Println(ui.Label("снято") + ui.Dim("  метка, маскарад, таблица маршрутов; программы под группой снова пойдут через VPN"))
	return nil
}

func sysctlRestore(key string) error {
	memo := strings.ReplaceAll(key, ".", "_")
	prev, ok := recall(memo)
	if !ok {
		return nil
	}
	if out, err := privileged.Run("sysctl", "-w", key+"="+prev); err != nil {
		return ui.Errorf("%s не вернулся в %s: %s", key, prev, firstLine(out, err))
	}
	forget(memo)
	return nil
}

// linuxStatus: правила netfilter, ip rule и таблица.
func linuxStatus() {
	ipt, err := iptables()
	if err != nil {
		ui.Println(ui.Dim(err.Error()))
		return
	}
	mangle, _ := privileged.Run(ipt, "-t", "mangle", "-S", "OUTPUT")
	nat, _ := privileged.Run(ipt, "-t", "nat", "-S", "POSTROUTING")
	marked := strings.Contains(mangle, "--gid-owner") && strings.Contains(mangle, linuxMark)
	masq := strings.Contains(nat, linuxMark) && strings.Contains(nat, "MASQUERADE")
	ui.Println(ui.Label("netfilter ") + ui.Dash(map[bool]string{true: "метка по группе стоит", false: "метки по группе нет"}[marked]) +
		ui.Dim(", маскарад: "+map[bool]string{true: "есть", false: "нет"}[masq]))
	rules, _ := runner.Output("ip", "rule", "show")
	hasRule := strings.Contains(rules, "fwmark "+linuxMark)
	table, _ := runner.Output("ip", "route", "show", "table", linuxTable)
	ui.Println(ui.Label("маршрут по метке ") + map[bool]string{true: "есть", false: "нет"}[hasRule] + ui.Dim("  таблица "+linuxTable+": "+ui.Dash(strings.TrimSpace(table))))
	phys, _ := Physical()
	ui.Println(ui.Label("rp_filter ") + ui.Dash(sysctlGet("net.ipv4.conf."+phys.Device+".rp_filter")) + ui.Dim(" ("+phys.Device+"), all "+ui.Dash(sysctlGet("net.ipv4.conf.all.rp_filter"))))
	if marked && masq && hasRule && strings.TrimSpace(table) != "" {
		ui.Println(ui.OK("правила на месте: процессы группы идут через " + phys.Device))
	}
}
