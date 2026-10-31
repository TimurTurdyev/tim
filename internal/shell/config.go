package shell

import (
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/TimurTurdyev/tim/internal/config"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// printConfig: где конфиг и что в нём - сводкой; весь файл - с raw.
func printConfig(cfg *config.Config, raw bool) error {
	if raw {
		ui.Println(ui.Dim(config.Path()))
		return toml.NewEncoder(ui.Writer()).Encode(cfg)
	}
	label := func(s string) string { return ui.Label(ui.Pad(s, 10)) }
	ui.Println(label("конфиг") + config.Path())
	ui.Println(label("код") + cfg.Dev + ui.Dim(" - здесь Диск ищет node_modules, vendor и сборки"))
	if len(cfg.Hosts) > 0 {
		ui.Println(label("обход") + ui.Count(len(cfg.Hosts), "цель", "цели", "целей") + ui.Dim(" - tim route"))
	}
	if sites := cfg.SiteList(); len(sites) > 0 {
		ui.Println(label("сайты") + strings.Join(sites, ", "))
	}
	sh := []string{"сводка при входе " + onOff(cfg.Shell.Motd)}
	if len(cfg.Shell.Aliases) > 0 {
		sh = append(sh, "алиасов "+strconv.Itoa(len(cfg.Shell.Aliases)))
	}
	ui.Println(label("оболочка") + strings.Join(sh, ", "))
	if legacy := cfg.Legacy(); legacy != "" {
		ui.Println(label("лишнее") + ui.Dim(legacy+" - tim их больше не читает, можно убрать руками"))
	}
	ui.Println(ui.Dim("весь файл: tim config --raw; править можно руками, tim перечитывает сам"))
	return nil
}

func onOff(on bool) string {
	if on {
		return "да"
	}
	return "нет"
}
